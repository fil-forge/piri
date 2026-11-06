package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/fil-forge/filecoin-services/go/eip712"
	libforgesign "github.com/fil-forge/libforge/commands/pdp/sign"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/go-commp-utils/nonffi"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/ipfs/go-cid"
	"github.com/samber/lo"
	"github.com/yugabyte/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/fil-forge/piri/pkg/pdp/smartcontracts"
	"github.com/fil-forge/piri/pkg/pdp/tasks"
	"github.com/fil-forge/piri/pkg/pdp/types"
)

// TODO we need to define non-retryable errors for the add root method, like lack of auth, and lack of dataset else this retries ~50 times
// TODO: Enhanced Crash Recovery Using Deterministic Request IDs:
/*

 Current Implementation Gap:
 There's a window between Send() completing and the database transaction
 where a crash would result in a sent transaction with no database record.
 On restart, duplicate detection wouldn't find anything, leading to duplicate submissions.

 Proposed Solution: Deterministic Request IDs

 Generate a deterministic hash of the request BEFORE calling Send:
   requestID := sha256(proofSetID + sorted(rootCIDs))

 Implementation approach:
 1. Before Send (around line 563):
    // Generate deterministic request ID
    sortedRoots := make([]string, len(rootCIDs))
    copy(sortedRoots, rootCIDs)
    sort.Strings(sortedRoots)

    requestID := sha256.Sum256([]byte(
        fmt.Sprintf("%d:%s", proofSetID, strings.Join(sortedRoots, ","))
    ))
    requestIDHex := "0x" + hex.EncodeToString(requestID[:])

 2. Create DB records with requestID as placeholder txHash:
    if err := p.db.Transaction(func(tx *gorm.DB) error {
        // Insert with deterministic ID
        mw := models.MessageWaitsEth{
            SignedTxHash: requestIDHex,
            TxStatus:     "preparing",
        }
        tx.Create(&mw)

        // Create pdp_proofset_root_adds with requestIDHex
        for _, addReq := range request {
            // ... create records using requestIDHex as AddMessageHash
        }
        return nil
    }); err != nil {
        return common.Hash{}, err
    }

 3. NOW safe to Send - even if we crash, records exist:
    txHash, err := p.sender.Send(ctx, p.address, txEth, reason)

 4. Update records with actual txHash:
    db.Model(&models.MessageWaitsEth{}).
        Where("signed_tx_hash = ?", requestIDHex).
        Update("signed_tx_hash", txHash.Hex())
    // Similarly update pdp_proofset_root_adds

 5. Update duplicate detection to check for both:
    WHERE add_message_hash = ? OR add_message_hash = ?
    -- Check for real txHash OR deterministic requestID

 Benefits:
 - Always leaves a trace in the database before Send
 - Deterministic ID is reproducible from same inputs
 - No special handling needed - looks like a normal hash
 - Handles all crash scenarios:
   * Before DB insert: No trace, safe to proceed
   * After DB insert, before Send: requestID exists, detected as duplicate
   * After Send, before update: Real tx on chain, requestID in DB, still detected
   * After update: Normal state with real txHash

 This approach is simpler than:
 - Parsing encoded transaction data from message_sends_eth
 - Modifying Send to work within transactions (deadlock issues)
 - Adding new tables or complex state management
*/

func (p *PDPService) AddRoots(ctx context.Context, id uint64, request []types.RootAdd) (res common.Hash, retErr error) {
	ctx, span := tracer.Start(ctx, "AddRoots", trace.WithAttributes(
		attribute.Int64("dataset.id", int64(id)),
		attribute.Int("roots.count", len(request)),
	))
	log.Infow("adding roots", "id", id, "request", request)
	defer func() {
		if retErr != nil {
			log.Errorw("failed to add roots", "id", id, "request", request, "err", retErr)
			span.RecordError(retErr)
			span.SetStatus(codes.Error, "failed to add roots")
		} else {
			span.SetAttributes(attribute.Stringer("tx", res))
			log.Infow("added roots", "id", id, "request", request, "response", res)
		}
		span.End()
	}()

	// Check if the proof set exists and belongs to this service
	var dsService string
	if err := p.db.QueryRow(ctx, `SELECT service FROM pdp_data_sets WHERE id = $1`, id).Scan(&dsService); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return common.Hash{}, types.NewErrorf(types.KindNotFound,
				"proof set %d does not exist. Must create a proof set first using CreateProofSet before adding roots", id)
		}
		return common.Hash{}, fmt.Errorf("failed to check if proof set exists: %w", err)
	}
	if dsService != p.name {
		return common.Hash{}, types.NewError(types.KindUnauthorized, "not authorized")
	}

	if len(request) == 0 {
		return common.Hash{}, types.NewErrorf(types.KindInvalidInput, "must provide at least one root")
	}

	// Collect all subrootCIDs to fetch their info in a batch
	newSubroots := cid.NewSet()
	for _, addRootReq := range request {
		if !addRootReq.Root.Defined() {
			return common.Hash{}, types.NewErrorf(types.KindInvalidInput, "must provide a root CID to add")
		}

		if len(addRootReq.SubRoots) == 0 {
			return common.Hash{}, types.NewErrorf(types.KindInvalidInput, "must provide at least one subroot CID to add")
		}

		for _, subrootEntry := range addRootReq.SubRoots {
			if !subrootEntry.Defined() {
				return common.Hash{}, types.NewErrorf(types.KindInvalidInput, "subroot CID is required for each subroot")
			}
			if newSubroots.Has(subrootEntry) {
				return common.Hash{}, types.NewErrorf(types.KindInvalidInput, "subroot CID %s is duplicated", subrootEntry.String())
			}

			newSubroots.Add(subrootEntry)
		}
	}

	// Calculate wait duration for transaction confirmations (used for both new and existing transactions)
	waitDuration := (tasks.MinConfidence + 2) * smartcontracts.FilecoinEpoch

	// Check if any of these roots have already been successfully added to prevent duplicate submissions
	// This handles the case where the node crashes after sending the transaction but before
	// recording it in the database, or when roots have already been fully processed.
	//
	// NB: pdp_data_set_piece_adds / pdp_data_set_pieces store v1 CommP CIDs —
	// Curio's convention (insertPieceAdds writes pieceCIDv1/subPieceCIDv1, and
	// the prove task converts sub_piece via CIDToPieceCommitmentV1) — so both
	// these guard queries and the inserts below use the v1 form.
	rootCIDs := make([]string, len(request))
	for i, req := range request {
		v1, err := asPieceCIDv1(req.Root)
		if err != nil {
			return common.Hash{}, fmt.Errorf("converting root %s to v1 piece CID: %w", req.Root, err)
		}
		rootCIDs[i] = v1.String()
	}

	log.Debugw("Checking for duplicate root submissions",
		"proofset_id", id,
		"root_count", len(rootCIDs))

	// Partition the request PER ROOT so AddRoots is idempotent. A batch may
	// mix roots that are already proven-tracked, roots owned by an in-flight
	// transaction, and genuinely new roots — e.g. the manager's non-atomic
	// enqueue+clear can re-enqueue an already-submitted root next to new
	// ones. (An earlier any-match guard returned early on the first overlap
	// and silently dropped the rest of the batch from proving.)
	//
	//  - completed (pdp_data_set_pieces): drop — already on-chain.
	//  - in-flight (pdp_data_set_piece_adds whose tx is pending or
	//    confirmed-successful): drop — the AddRoots call that sent that tx
	//    blocks in WaitForConfirmation below and its caller retries on
	//    failure, so that call vouches for those roots.
	//  - dead (piece_adds rows whose tx confirmed-but-reverted or failed):
	//    clear the stale rows and treat their roots as new — otherwise they
	//    trap every retry in a wait-on-a-dead-tx loop and never resubmit.
	//  - new: submitted below.

	var completedRoots []struct {
		Root           string `db:"piece"`
		AddMessageHash string `db:"add_message_hash"`
	}
	if err := p.db.Select(ctx, &completedRoots, `
		SELECT DISTINCT piece, add_message_hash
		FROM pdp_data_set_pieces
		WHERE data_set = $1 AND piece = ANY($2)
	`, id, rootCIDs); err != nil {
		return common.Hash{}, fmt.Errorf("failed to check for existing completed roots: %w", err)
	}
	completed := make(map[string]string, len(completedRoots))
	for _, r := range completedRoots {
		completed[r.Root] = r.AddMessageHash
	}

	var stagedRoots []struct {
		Root           string `db:"piece"`
		AddMessageHash string `db:"add_message_hash"`
		TxStatus       string `db:"tx_status"`
		TxSuccess      *bool  `db:"tx_success"`
	}
	if err := p.db.Select(ctx, &stagedRoots, `
		SELECT DISTINCT a.piece, a.add_message_hash, m.tx_status, m.tx_success
		FROM pdp_data_set_piece_adds a
		JOIN message_waits_eth m ON m.signed_tx_hash = a.add_message_hash
		WHERE a.data_set = $1 AND a.piece = ANY($2)
	`, id, rootCIDs); err != nil {
		return common.Hash{}, fmt.Errorf("failed to check for staged roots: %w", err)
	}
	inflight := make(map[string]string)
	deadTxs := make(map[string]struct{})
	for _, r := range stagedRoots {
		txDead := r.TxStatus == "failed" ||
			(r.TxStatus == "confirmed" && r.TxSuccess != nil && !*r.TxSuccess)
		if txDead {
			deadTxs[r.AddMessageHash] = struct{}{}
			continue
		}
		// A root staged under several txs (prior retries) stays in-flight as
		// long as at least one of them is live.
		inflight[r.Root] = r.AddMessageHash
	}

	if len(deadTxs) > 0 {
		// Delete by tx hash, not by requested piece: a failed/reverted tx is
		// dead for every piece it staged, including pieces outside this
		// request. The watcher only promotes piece_adds -> pieces on confirmed
		// success, so rows under a dead tx can never progress; scoping the
		// delete to this request's pieces would leave a half-cleared tx whose
		// remaining rows linger until (unless) a later call happens to include
		// them. Clearing the whole tx loses nothing — any caller retrying
		// those other pieces finds no staged rows and resubmits them as new.
		// A piece staged under both a dead tx and a live one keeps its live
		// rows and stays classified in-flight.
		//
		// NB: stock Curio never deletes these rows — a trigger marks them
		// add_message_ok = FALSE and every Curio consumer (addpiece watcher,
		// piece GC, dataset verify) filters them out as permanent tombstones.
		// Deleting instead of tombstoning is safe here because this guard is
		// the only reader that would ever act on them, and it keeps repeated
		// AddRoots calls from re-fetching an ever-growing dead row-set.
		hashes := lo.Keys(deadTxs)
		log.Warnw("clearing piece-add rows from failed AddRoots transactions; their roots resubmit",
			"proofset_id", id, "dead_txs", hashes)
		if _, err := p.db.Exec(ctx, `
			DELETE FROM pdp_data_set_piece_adds
			WHERE data_set = $1 AND add_message_hash = ANY($2)
		`, id, hashes); err != nil {
			return common.Hash{}, fmt.Errorf("failed to clear dead piece-add rows: %w", err)
		}
	}

	// Filter the request down to the genuinely new roots.
	newRequest := make([]types.RootAdd, 0, len(request))
	for i, req := range request {
		v1 := rootCIDs[i]
		if _, ok := completed[v1]; ok {
			continue
		}
		if _, ok := inflight[v1]; ok {
			continue
		}
		newRequest = append(newRequest, req)
	}

	if len(newRequest) == 0 {
		// Nothing new to submit. If roots ride in-flight txs (the pure-retry
		// case), block on confirmation so the caller's retry loop still owns
		// eventual delivery: a failure here errors the job, and the next
		// attempt clears the then-dead rows and resubmits.
		inflightTxs := make(map[string]struct{}, len(inflight))
		for _, h := range inflight {
			inflightTxs[h] = struct{}{}
		}
		var last common.Hash
		for h := range inflightTxs {
			span.AddEvent("waiting on in-flight add roots tx")
			txHash := common.HexToHash(h)
			log.Infow("all requested roots already staged; waiting for in-flight transaction",
				"proofset_id", id, "tx_hash", h, "wait_duration", waitDuration)
			if err := p.WaitForConfirmation(ctx, txHash, waitDuration); err != nil {
				log.Errorw("existing AddRoots transaction failed or timed out",
					"error", err, "tx_hash", h, "proofset_id", id)
				return txHash, fmt.Errorf("existing transaction %s failed or timed out: %w", h, err)
			}
			last = txHash
		}
		if len(inflightTxs) > 0 {
			return last, nil
		}
		// Every requested root is already fully processed.
		for _, h := range completed {
			log.Infow("all requested roots already added; nothing to submit",
				"proofset_id", id, "tx_hash", h)
			return common.HexToHash(h), nil
		}
		return common.Hash{}, fmt.Errorf("internal: empty root partition for non-empty request")
	}

	if len(newRequest) < len(request) {
		log.Infow("request partially overlaps already-staged roots; submitting only the new roots",
			"proofset_id", id,
			"requested", len(request),
			"completed", len(completed),
			"in_flight", len(inflight),
			"submitting", len(newRequest))
	}
	request = newRequest

	// Rebuild the subroot working set for the (possibly filtered) request.
	newSubroots = cid.NewSet()
	for _, addReq := range request {
		for _, subrootEntry := range addReq.SubRoots {
			newSubroots.Add(subrootEntry)
		}
	}

	// Map to store subrootCID -> [pieceInfo, pdp_pieceref.id, subrootOffset, rawSize]
	type SubrootInfo struct {
		PieceInfo     abi.PieceInfo
		PDPPieceRefID int64
		SubrootOffset uint64
		RawSize       uint64 // Actual unpadded data size (not derived from padded size)
	}

	type subrootRow struct {
		PieceCID        string `db:"piece_cid"`
		PDPPieceRefID   int64  `db:"pdp_piece_ref_id"`
		PieceRefID      int64  `db:"piece_ref"`
		PiecePaddedSize uint64 `db:"piece_padded_size"`
		PieceRawSize    int64  `db:"piece_raw_size"`
	}

	// Convert set to slice of string for db query
	newSubrootsList := lo.Map(newSubroots.Keys(), func(c cid.Cid, _ int) string {
		return c.String()
	})

	var rows []subrootRow
	if err := p.db.Select(ctx, &rows, `
		SELECT ppr.piece_cid, ppr.id AS pdp_piece_ref_id, ppr.piece_ref,
		       pp.piece_padded_size, pp.piece_raw_size
		FROM pdp_piecerefs ppr
		JOIN parked_piece_refs pprf ON pprf.ref_id = ppr.piece_ref
		JOIN parked_pieces pp ON pp.id = pprf.piece_id
		WHERE ppr.service = $1 AND ppr.piece_cid = ANY($2)
	`, p.name, newSubrootsList); err != nil {
		return common.Hash{}, err
	}

	subrootInfoMap := make(map[cid.Cid]*SubrootInfo)
	currentSubroots := cid.NewSet()
	for _, r := range rows {
		// Decode the piece CID.
		decodedCID, err := cid.Decode(r.PieceCID)
		if err != nil {
			return common.Hash{}, fmt.Errorf("invalid piece CID in database: %s", r.PieceCID)
		}
		subrootInfoMap[decodedCID] = &SubrootInfo{
			PieceInfo: abi.PieceInfo{
				Size:     abi.PaddedPieceSize(r.PiecePaddedSize),
				PieceCID: decodedCID,
			},
			PDPPieceRefID: r.PDPPieceRefID,
			SubrootOffset: 0, // will be computed below
			RawSize:       uint64(r.PieceRawSize),
		}
		currentSubroots.Add(decodedCID)
	}

	// Ensure every requested subrootCID was found.
	if err := currentSubroots.ForEach(func(c cid.Cid) error {
		if !newSubroots.Has(c) {
			return fmt.Errorf("subroot CID %s not found or does not belong to service %s", c.String(), p.name)
		}
		return nil
	}); err != nil {
		return common.Hash{}, err
	}

	// For each AddRootRequest, validate the provided RootCID.
	for _, addReq := range request {
		// Reset offset for each root so subroots start at 0 for each root
		var totalOffset uint64 = 0
		// Collect pieceInfos for each subroot.
		pieceInfos := make([]abi.PieceInfo, len(addReq.SubRoots))

		for i, subCID := range addReq.SubRoots {
			subInfo, exists := subrootInfoMap[subCID]
			if !exists {
				return common.Hash{}, fmt.Errorf("subroot CID %s not found in subroot info map", subCID)
			}
			subInfo.SubrootOffset = totalOffset
			pieceInfos[i] = subInfo.PieceInfo
			totalOffset += uint64(subInfo.PieceInfo.Size)
		}

		// GenerateUnsealedCID requires v1PieceCID, so transform here
		var v1SubInfos []abi.PieceInfo
		for _, pi := range pieceInfos {
			v1PieceCID, err := asPieceCIDv1(pi.PieceCID)
			if err != nil {
				return common.Hash{}, err
			}
			v1SubInfos = append(v1SubInfos, abi.PieceInfo{
				Size:     pi.Size,
				PieceCID: v1PieceCID,
			})
		}

		// Generate the unsealed CID from the collected piece infos.
		proofType := abi.RegisteredSealProof_StackedDrg64GiBV1_1
		generatedCID, err := nonffi.GenerateUnsealedCID(proofType, v1SubInfos)
		if err != nil {
			return common.Hash{}, fmt.Errorf("failed to generate RootCID: %w", err)
		}

		// turn the uploaded roots into PieceCIDV1
		providedPieceCidV1, err := asPieceCIDv1(addReq.Root)
		if err != nil {
			return common.Hash{}, fmt.Errorf("failed to generate PieceCIDV1 for request: %w", err)
		}

		// Compare the generated and provided CIDs.
		if !providedPieceCidV1.Equals(generatedCID) {
			return common.Hash{}, fmt.Errorf("provided RootCID does not match generated RootCID: %s (v1 %s) != %s",
				addReq.Root, providedPieceCidV1, generatedCID)
		}
		span.AddEvent("root", trace.WithAttributes(attribute.Stringer("root", addReq.Root)))
	}

	// Step 5: Prepare the Ethereum transaction data outside the DB transaction
	// Obtain the ABI of the PDPVerifier contract
	abiData, err := p.verifierContract.GetABI()
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to get abi data from PDPVerifierMetaData: %w", err)
	}

	// Prepare PieceData array for Ethereum transaction
	// Use the generated contract binding type
	var pieceDataArray []smartcontracts.CidsCid

	for _, addRootReq := range request {
		// Convert RootCID to bytes
		rootCID := addRootReq.Root

		_, rawSize, err := commcid.PieceCidV1FromV2(rootCID)
		if err != nil {
			return common.Hash{}, fmt.Errorf("invalid PieceCIDV2: %w", err)
		}
		height, _, err := commcid.PayloadSizeToV1TreeHeightAndPadding(rawSize)
		if err != nil {
			return common.Hash{}, fmt.Errorf("computing height and padding: %w", err)
		}
		// NB: defined here: https://github.com/FilOzone/pdp/blob/main/src/PDPVerifier.sol#L44
		maxPieceSizeLog2, err := p.cachedMaxPieceSizeLog2(ctx)
		if err != nil {
			return common.Hash{}, err
		}
		if uint64(height) > maxPieceSizeLog2.Uint64() {
			return common.Hash{}, fmt.Errorf("invalid height: %d", height)
		}

		var totalSize uint64 = 0
		var prevSubrootSize = subrootInfoMap[addRootReq.SubRoots[0]].PieceInfo.Size
		for i, subrootEntry := range addRootReq.SubRoots {
			subrootInfo := subrootInfoMap[subrootEntry]
			if subrootInfo.PieceInfo.Size > prevSubrootSize {
				// implies a bad request
				return common.Hash{}, fmt.Errorf("subroots must be in descending order of size, root %d %s is larger than prev subroot %s", i, subrootEntry, addRootReq.SubRoots[i-1])
			}
			prevSubrootSize = subrootInfo.PieceInfo.Size

			paddedSize := uint64(subrootInfo.PieceInfo.Size)
			totalSize += paddedSize
		}

		// Prepare RootData for Ethereum transaction using the generated binding type
		rootData := smartcontracts.CidsCid{
			Data: rootCID.Bytes(),
		}

		pieceDataArray = append(pieceDataArray, rootData)
	}

	// Convert proofSetID to *big.Int for contract calls
	proofSetID := new(big.Int).SetUint64(id)

	// Get dataset info to obtain the clientDataSetId
	datasetInfo, err := p.serviceContract.GetDataSet(ctx, proofSetID)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to get dataset info: %w", err)
	}

	// Convert pieceDataArray to [][]byte for signing
	pieceDataBytes := make([][]byte, len(pieceDataArray))
	for i, piece := range pieceDataArray {
		pieceDataBytes[i] = piece.Data
	}

	// Prepare metadata arrays (one empty array per piece for now)
	metadata := make([][]eip712.MetadataEntry, len(pieceDataArray))
	for i := range metadata {
		metadata[i] = []eip712.MetadataEntry{}
	}

	// TODO(forrest)[ucan1] #9: rebuild proof bundling on top of ucantone receipts +
	// libforge pdp/sign.PieceProofs. The previous go-ucanto path collected
	// blob/accept and pdp/accept invocations + receipts into per-piece agent
	// messages; that requires the receipt/acceptance stores to be migrated
	// first. Until then we send empty piece proofs and a nil proof
	// container — the signing service still issues an EIP-712 signature,
	// just without the attestation chain.
	pieceProofs := make([]libforgesign.PieceProofs, 0, len(request))
	for _, req := range request {
		pieceProofs = append(pieceProofs, libforgesign.PieceProofs{
			Proofs: make([]cid.Cid, 0, len(req.SubRoots)),
		})
	}

	// Request a signature for adding pieces from the signing service.
	// Use clientDataSetId from FilecoinWarmStorageService (not PDPVerifier's setId).
	// Generate a random nonce so it never collides with values stored in clientNonces during createDataSet.
	nonceBytes := make([]byte, 32)
	if _, err := rand.Read(nonceBytes); err != nil {
		return common.Hash{}, fmt.Errorf("failed to generate nonce: %w", err)
	}
	nonce := new(big.Int).SetBytes(nonceBytes)
	// TODO(ash/forrst): nil is bad mkay, don't do this in the release....
	signature, err := p.signingService.SignAddPieces(ctx,
		p.id,
		datasetInfo.ClientDataSetId, // Use FilecoinWarmStorageService clientDataSetId
		nonce,                       // client-chosen nonce, disjoint from createDataSet clientDataSetId
		pieceDataBytes,
		metadata,
		pieceProofs,
		nil, // proofContainer — see TODO above
		nil, // proofs (access delegation) — signing-service obtains its own
	)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to sign AddPieces: %w", err)
	}

	// Encode the extraData with signature and metadata
	extraDataBytes, err := p.edc.EncodeAddPiecesExtraData(nonce, signature, metadata)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to encode extraData: %w", err)
	}

	// listener must be empty address for datasets that already exist, thus 3rd argument.
	data, err := abiData.Pack("addPieces", proofSetID, common.Address{}, pieceDataArray, extraDataBytes)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to pack addRoots: %w", err)
	}

	// Prepare the transaction (nonce will be set to 0, SenderETH will assign it)
	txEth := ethtypes.NewTransaction(
		0,
		p.cfg.Contracts.Verifier,
		big.NewInt(0),
		0,
		nil,
		data,
	)

	// Step 8: Send the transaction using SenderETH
	reason := "pdp-addroots"
	txHash, err := p.sender.Send(ctx, p.address, txEth, reason)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to send transaction: %w", err)
	}
	span.AddEvent("transaction sent")

	// Step 9: Insert into message_waits_eth and pdp_data_set_piece_adds.
	// Mirror Curio's handleAddPieceToDataSet + insertPieceAdds write-shape.
	// Use lowercased tx hash to match Curio's storage convention.
	txHashLower := strings.ToLower(txHash.Hex())
	comm, err := p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		// Insert into message_waits_eth
		n, err := tx.Exec(`
            INSERT INTO message_waits_eth (signed_tx_hash, tx_status)
            VALUES ($1, $2)
        `, txHashLower, "pending")
		if err != nil {
			return false, err
		}
		if n != 1 {
			return false, fmt.Errorf("expected 1 row in message_waits_eth, got %d", n)
		}

		// Update proof set for initialization upon first add (idempotent)
		if _, err := tx.Exec(`
			UPDATE pdp_data_sets SET init_ready = true
			WHERE id = $1
			  AND prev_challenge_request_epoch IS NULL
			  AND challenge_request_msg_hash IS NULL
			  AND prove_at_epoch IS NULL
		`, id); err != nil {
			return false, err
		}

		// Insert into pdp_data_set_piece_adds — mirror Curio insertPieceAdds.
		// add_message_index = piece index (outer loop). data_set = id (known, non-null).
		// piece/sub_piece are stored as v1 CommP CIDs (Curio's convention; the
		// prove task's CIDToPieceCommitmentV1 rejects v2 piece CIDs).
		for addMessageIndex, addReq := range request {
			rootV1, err := asPieceCIDv1(addReq.Root)
			if err != nil {
				return false, fmt.Errorf("converting root %s to v1 piece CID: %w", addReq.Root, err)
			}
			for _, subrootEntry := range addReq.SubRoots {
				subInfo := subrootInfoMap[subrootEntry]
				subV1, err := asPieceCIDv1(subrootEntry)
				if err != nil {
					return false, fmt.Errorf("converting subroot %s to v1 piece CID: %w", subrootEntry, err)
				}
				n, err := tx.Exec(`
                    INSERT INTO pdp_data_set_piece_adds (
                        data_set,
                        piece,
                        add_message_hash,
                        add_message_index,
                        sub_piece,
                        sub_piece_offset,
                        sub_piece_size,
                        pdp_pieceref
                    ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
                `,
					id,
					rootV1.String(),
					txHashLower,
					addMessageIndex,
					subV1.String(),
					subInfo.SubrootOffset,
					uint64(subInfo.PieceInfo.Size), // sub_piece_size = padded size, matches Curio
					subInfo.PDPPieceRefID,
				)
				if err != nil {
					return false, err
				}
				if n != 1 {
					return false, fmt.Errorf("expected 1 row in pdp_data_set_piece_adds, got %d", n)
				}
			}
		}

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		log.Errorw("Failed to insert into database", "error", err, "txHash", txHashLower, "subroots", subrootInfoMap)
		return common.Hash{}, fmt.Errorf("failed to insert into database: %w", err)
	}
	if !comm {
		return common.Hash{}, fmt.Errorf("failed to commit add pieces tracking")
	}

	// Step 10: Wait for the transaction to be confirmed on chain
	// This prevents the race condition where multiple parallel AddRoots calls
	// all read the same nextPieceId but only one can succeed.
	// Rows were written with the lowercased hash, so wait on that form.
	confirmHash := common.HexToHash(txHashLower)
	log.Infow("waiting for AddRoots transaction confirmation", "txHash", txHashLower, "proofSetID", id, "waitDuration", waitDuration)
	if err := p.WaitForConfirmation(ctx, confirmHash, waitDuration); err != nil {
		log.Errorw("AddRoots transaction failed or timed out", "error", err, "txHash", txHashLower, "proofSetID", id)
		return confirmHash, fmt.Errorf("transaction %s failed or timed out: %w", txHashLower, err)
	}

	log.Infow("AddRoots transaction confirmed successfully", "txHash", txHashLower, "proofSetID", id)
	return confirmHash, nil
}

// TODO(phase 5a): the proof-bundling helper that lived here built per-piece
// agent messages from go-ucanto blob/accept and pdp/accept invocations +
// receipts. Migrating it depends on the receipt and acceptance stores
// being moved to ucantone first. The body has been removed; reintroduce a
// ucantone-shaped equivalent that returns ([]cid.Cid /* blob/accept
// task links */, ucan.Container /* invocations + pdp/accept receipts */,
// error) when those stores migrate.
