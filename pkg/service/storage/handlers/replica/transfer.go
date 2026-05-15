package replica

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/fil-forge/libforge/capabilities"
	assertcaps "github.com/fil-forge/libforge/capabilities/assert"
	blobcaps "github.com/fil-forge/libforge/capabilities/blob"
	"github.com/fil-forge/libforge/capabilities/blob/replica"
	pdpcaps "github.com/fil-forge/libforge/capabilities/pdp"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/principal"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/fil-forge/ucantone/ucan/promise"
	"github.com/fil-forge/ucantone/ucan/receipt"
	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"

	"github.com/fil-forge/piri/pkg/pdp"
	"github.com/fil-forge/piri/pkg/service/blobs"
	"github.com/fil-forge/piri/pkg/service/claims"
	blobhandler "github.com/fil-forge/piri/pkg/service/storage/handlers/blob"
	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/receiptstore"
)

var log = logging.Logger("storage/handlers/replica")

// TransferService is the surface the replica transfer handler depends on.
type TransferService interface {
	// ID is the storage service identity, used to sign UCAN invocations and receipts.
	ID() principal.Signer
	// PDP handles PDP aggregation
	PDP() pdp.PDP
	// Blobs provides access to the blobs service.
	Blobs() blobs.Blobs
	// Claims provides access to the claims service.
	Claims() claims.Claims
	// Receipts provides access to receipts
	Receipts() receiptstore.ReceiptStore
	// UploadConnection is the outbound RPC endpoint of the upload service.
	// During the UCAN 1.0 migration this is `any`-typed pending Phase 7a
	// wiring of the ucantone client into the config layer; the
	// receipt-forwarding step is logged-and-skipped rather than sent.
	UploadConnection() any
}

// TransferSource describes the upstream the blob is being replicated from.
type TransferSource struct {
	// ID is the principal that signed the source's location commitment.
	ID did.DID
	// URL the blob may be requested from.
	URL url.URL
}

type transferSourceModel struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

// TransferRequest captures everything Transfer needs to execute one replica
// transfer. It is JSON-encoded into the replication job queue, so the on-wire
// shape must round-trip via Marshal/Unmarshal.
type TransferRequest struct {
	// Space is the space to associate with blob.
	Space did.DID
	// Blob is the blob in question.
	Blob blobcaps.Blob
	// Source is the location to replicate the blob from.
	Source TransferSource
	// Sink is the location to replicate the blob to. nil if the blob is
	// already on this storage node and only requires assertion issuance.
	Sink *url.URL
	// Cause is the invocation responsible for spawning this replication
	// (should be a /blob/replica/transfer invocation).
	Cause *invocation.Invocation
}

type transferRequestModel struct {
	Space  string              `json:"space"`
	Blob   blobcaps.Blob       `json:"blob"`
	Source transferSourceModel `json:"source"`
	Sink   *string             `json:"sink,omitempty"`
	Cause  []byte              `json:"cause"`
}

func (t *TransferRequest) MarshalJSON() ([]byte, error) {
	aux := transferRequestModel{
		Space: t.Space.String(),
		Blob:  t.Blob,
		Source: transferSourceModel{
			ID:  t.Source.ID.String(),
			URL: t.Source.URL.String(),
		},
	}
	if t.Sink != nil {
		sinkStr := t.Sink.String()
		aux.Sink = &sinkStr
	}
	if t.Cause != nil {
		aux.Cause = t.Cause.Bytes()
	}
	return json.Marshal(aux)
}

func (t *TransferRequest) UnmarshalJSON(b []byte) error {
	aux := transferRequestModel{}
	if err := json.Unmarshal(b, &aux); err != nil {
		return fmt.Errorf("unmarshaling TransferRequest: %w", err)
	}

	spaceDID, err := did.Parse(aux.Space)
	if err != nil {
		return fmt.Errorf("parsing space DID: %w", err)
	}
	t.Space = spaceDID
	t.Blob = aux.Blob

	sourceID, err := did.Parse(aux.Source.ID)
	if err != nil {
		return fmt.Errorf("parsing source DID: %w", err)
	}
	t.Source.ID = sourceID
	sourceURL, err := url.Parse(aux.Source.URL)
	if err != nil {
		return fmt.Errorf("parsing source URL: %w", err)
	}
	t.Source.URL = *sourceURL

	if aux.Sink != nil {
		sinkURL, err := url.Parse(*aux.Sink)
		if err != nil {
			return fmt.Errorf("parsing sink URL: %w", err)
		}
		t.Sink = sinkURL
	}

	if len(aux.Cause) > 0 {
		inv, err := invocation.Decode(aux.Cause)
		if err != nil {
			return fmt.Errorf("unmarshaling cause invocation: %w", err)
		}
		t.Cause = inv
	}

	return nil
}

// Transfer handles blob replication with idempotent retry semantics. The
// function is called from a job queue with up to N retries; each retry
// re-runs the full sequence and tolerates idempotency at every step:
//
//  1. Check whether the blob already exists locally (PDP-backed or in the
//     blob store). If so, skip the network transfer.
//  2. If a sink URL is present and the blob is missing, fetch the blob bytes
//     from the source URL (plain HTTP GET — the previous authorized-retrieval
//     flow used the legacy `/access/grant` exchange whose libforge
//     replacement is the Phase 7b `/access/delegate` handler), then PUT
//     them to the sink.
//  3. Run /blob/accept locally to record the new replica.
//  4. Issue and store a /blob/replica/transfer receipt. The receipt's `Site`
//     points at the location commitment claim emitted by /blob/accept;
//     `PDP` carries the /pdp/accept promise when PDP is enabled.
//
// The original implementation also forwarded the receipt to the upload
// service via a /ucan/conclude invocation. That step requires an ucantone
// client connection to the upload service, which is not yet wired through
// piri's config; for now we log and skip. Receipts are persisted locally and
// remain available via the receipt API.
func Transfer(ctx context.Context, service TransferService, request *TransferRequest, metrics *Metrics) (err error) {
	stopwatch := metrics.startDuration(sourceLabel(&request.Source.URL), sinkLabel(request.Sink))
	defer func() {
		if stopwatch != nil {
			stopwatch.Stop(ctx)
		}
	}()

	blobExists, err := checkBlobExists(ctx, service, request.Blob)
	if err != nil {
		return fmt.Errorf("checking blob existence: %w", err)
	}

	var (
		claim  *invocation.Invocation
		pdpInv *invocation.Invocation
	)

	if request.Sink != nil && !blobExists {
		claim, pdpInv, err = transferBlobFromSource(ctx, service, request)
		if err != nil {
			return fmt.Errorf("transferring blob from source: %w", err)
		}
	} else {
		claim, pdpInv, err = createLocationAssertion(ctx, service, request)
		if err != nil {
			return fmt.Errorf("creating location assertion: %w", err)
		}
	}

	if err := issueAndStoreTransferReceipt(ctx, service, request, claim, pdpInv); err != nil {
		return fmt.Errorf("issuing transfer receipt: %w", err)
	}

	// TODO(phase4): once a ucantone connection to the upload service is in
	// the config, wrap the receipt in a /ucan/conclude invocation and POST
	// it. Until then the receipt is only stored locally.
	log.Debugw("transfer receipt stored locally; upload-service forwarding pending Phase 4 client wiring",
		"blob", request.Blob.Digest,
	)
	return nil
}

// SendFailureReceipt issues a failure receipt for a failed transfer and
// stores it locally. Upload-service forwarding is logged and skipped (see
// Transfer above).
func SendFailureReceipt(ctx context.Context, service TransferService, request *TransferRequest, transferErr error) error {
	if request.Cause == nil {
		return errors.New("cannot issue failure receipt: TransferRequest.Cause is nil")
	}
	failErr := fmt.Errorf("replica transfer failed after retries: %w", transferErr)
	rcpt, err := receipt.IssueErr(service.ID(), request.Cause.Link(), namedErr{name: "TransferFailed", msg: failErr.Error()})
	if err != nil {
		return fmt.Errorf("issuing failure receipt: %w", err)
	}
	if err := service.Receipts().Put(ctx, rcpt); err != nil {
		return fmt.Errorf("storing failure receipt: %w", err)
	}
	log.Debugw("transfer failure receipt stored locally; upload-service forwarding pending Phase 4 client wiring",
		"blob", request.Blob.Digest, "err", transferErr,
	)
	return nil
}

// transferBlobFromSource fetches the blob from source, PUTs it to sink, and
// runs the local /blob/accept. Returns the location commitment + optional
// /pdp/accept invocations from the local accept response.
func transferBlobFromSource(ctx context.Context, service TransferService, request *TransferRequest) (*invocation.Invocation, *invocation.Invocation, error) {
	// Plain HTTP GET from source. The authorized-retrieval path (which
	// would invoke /blob/retrieve with an /access/delegate-issued
	// delegation) is pending Phase 7b. Public sources work today; private
	// ones return 401/403 and the failure receipt path runs.
	sourceReq, err := http.NewRequestWithContext(ctx, http.MethodGet, request.Source.URL.String(), nil)
	if err != nil {
		return nil, nil, fmt.Errorf("creating source request: %w", err)
	}
	sourceRes, err := http.DefaultClient.Do(sourceReq)
	if err != nil {
		return nil, nil, fmt.Errorf("fetching from source: %w", err)
	}
	defer sourceRes.Body.Close()
	if sourceRes.StatusCode >= 300 || sourceRes.StatusCode < 200 {
		return nil, nil, fmt.Errorf("source returned status %d", sourceRes.StatusCode)
	}

	// PUT the body to sink.
	sinkReq, err := http.NewRequestWithContext(ctx, http.MethodPut, request.Sink.String(), sourceRes.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("creating sink request: %w", err)
	}
	// Forward through known headers (Content-Length is usually set by the
	// source; the presigned PUT URLs we issue expect it).
	for _, h := range []string{"Content-Length", "Content-Type"} {
		if v := sourceRes.Header.Get(h); v != "" {
			sinkReq.Header.Set(h, v)
		}
	}
	sinkRes, err := http.DefaultClient.Do(sinkReq)
	if err != nil {
		return nil, nil, fmt.Errorf("putting to sink: %w", err)
	}
	defer sinkRes.Body.Close()
	if sinkRes.StatusCode >= 300 || sinkRes.StatusCode < 200 {
		body, _ := io.ReadAll(sinkRes.Body)
		return nil, nil, fmt.Errorf("sink returned status %d: %s", sinkRes.StatusCode, string(body))
	}

	// Local /blob/accept.
	acceptResp, err := blobhandler.Accept(ctx, service, &blobhandler.AcceptRequest{
		Space: request.Space,
		Blob:  request.Blob,
		Put:   promise.AwaitOK{Task: request.Cause.Link()},
		Cause: request.Cause.Link(),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("local /blob/accept: %w", err)
	}
	return acceptResp.Claim, acceptResp.PDP, nil
}

// createLocationAssertion is the fast path when the blob is already locally
// present (or the request has no sink): we skip the network transfer and
// re-issue a fresh location commitment.
func createLocationAssertion(ctx context.Context, service TransferService, request *TransferRequest) (*invocation.Invocation, *invocation.Invocation, error) {
	var (
		loc          url.URL
		pdpAcceptInv *invocation.Invocation
	)

	if service.PDP() == nil {
		panic("pdp service required")
	}

	// PDP-backed path.
	has, err := service.PDP().API().Has(ctx, request.Blob.Digest)
	if err != nil {
		return nil, nil, fmt.Errorf("checking PDP for blob: %w", err)
	}
	if !has {
		return nil, nil, fmt.Errorf("piece not found")
	}
	blobCID := cid.NewCidV1(cid.Raw, request.Blob.Digest)
	loc, err = service.PDP().API().ReadPieceURL(blobCID)
	if err != nil {
		return nil, nil, fmt.Errorf("creating read-piece URL: %w", err)
	}
	pieceAccept, err := pdpcaps.Accept.Invoke(
		service.ID(),
		service.ID().DID(),
		&pdpcaps.AcceptArguments{Blob: request.Blob.Digest},
		invocation.WithNoExpiration(),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("creating /pdp/accept invocation: %w", err)
	}
	pdpAcceptInv = pieceAccept
	return issueLocationClaim(service, request, loc, pdpAcceptInv)
}

func issueLocationClaim(service TransferService, request *TransferRequest, loc url.URL, pdpAcceptInv *invocation.Invocation) (*invocation.Invocation, *invocation.Invocation, error) {
	byteRange := assertcaps.Range{Offset: 0, Length: &request.Blob.Size}
	claim, err := assertcaps.Location.Invoke(
		service.ID(),
		service.ID().DID(),
		&assertcaps.LocationArguments{
			Space:    request.Space,
			Content:  request.Blob.Digest,
			Location: []capabilities.CborURL{capabilities.CborURL(loc)},
			Range:    &byteRange,
		},
		invocation.WithNoExpiration(),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("creating location commitment: %w", err)
	}
	return claim, pdpAcceptInv, nil
}

// issueAndStoreTransferReceipt builds the /blob/replica/transfer receipt
// (linking to the freshly-issued location commitment claim and, optionally,
// the /pdp/accept invocation) and writes it to the receipt store.
func issueAndStoreTransferReceipt(
	ctx context.Context,
	service TransferService,
	request *TransferRequest,
	claim *invocation.Invocation,
	pdpInv *invocation.Invocation,
) error {
	ok := &replica.TransferOK{Site: claim.Link()}
	if pdpInv != nil {
		ok.PDP = promise.AwaitOK{Task: pdpInv.Link()}
	}
	rcpt, err := receipt.IssueOK(service.ID(), request.Cause.Link(), ok)
	if err != nil {
		return fmt.Errorf("issuing /blob/replica/transfer receipt: %w", err)
	}
	if err := service.Receipts().Put(ctx, rcpt); err != nil {
		return fmt.Errorf("storing transfer receipt: %w", err)
	}
	return nil
}

func checkBlobExists(ctx context.Context, service TransferService, blob blobcaps.Blob) (bool, error) {
	if service.PDP() != nil {
		has, err := service.PDP().API().Has(ctx, blob.Digest)
		if err != nil {
			return false, fmt.Errorf("resolving piece: %w", err)
		}
		return has, nil
	}
	_, err := service.Blobs().Store().Get(ctx, blob.Digest)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	return false, fmt.Errorf("checking blob exists: %w", err)
}

func sinkLabel(sink *url.URL) string {
	if sink == nil {
		return "none"
	}
	return sink.Host
}

func sourceLabel(source *url.URL) string {
	if source == nil {
		return "none"
	}
	return source.Host
}

// namedErr is a cborgen-marshalable error type with a stable name; used to
// build failure receipts via [receipt.IssueErr].
type namedErr struct {
	name string
	msg  string
}

func (e namedErr) Name() string  { return e.name }
func (e namedErr) Error() string { return e.msg }

// MarshalCBOR encodes a {name, message} map so the receipt's failure value is
// readable across the wire by callers that don't share this type definition.
func (e namedErr) MarshalCBOR(w io.Writer) error {
	// Manually emit a 2-entry map: {"name": <name>, "message": <msg>}.
	// cborgen doesn't generate code for unexported-field types, so we
	// produce the canonical shape here.
	if _, err := w.Write([]byte{0xa2}); err != nil { // map(2)
		return err
	}
	if err := writeCborString(w, "name"); err != nil {
		return err
	}
	if err := writeCborString(w, e.name); err != nil {
		return err
	}
	if err := writeCborString(w, "message"); err != nil {
		return err
	}
	if err := writeCborString(w, e.msg); err != nil {
		return err
	}
	return nil
}

func writeCborString(w io.Writer, s string) error {
	// short-string fast path (cborgen-style); strings up to 255 bytes use
	// 0x78 prefix, longer use 0x79/0x7a/0x7b — for our short names/messages
	// the short path is sufficient. Larger messages are truncated at the
	// receiver boundary anyway.
	const maxLen = 1 << 16
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	switch {
	case len(s) < 24:
		if _, err := w.Write([]byte{0x60 | byte(len(s))}); err != nil {
			return err
		}
	case len(s) < 256:
		if _, err := w.Write([]byte{0x78, byte(len(s))}); err != nil {
			return err
		}
	default:
		if _, err := w.Write([]byte{0x79, byte(len(s) >> 8), byte(len(s))}); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, s)
	return err
}
