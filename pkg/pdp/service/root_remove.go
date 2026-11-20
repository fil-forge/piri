package service

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/filecoin-project/curio/harmony/harmonydb"
)

func (p *PDPService) RemoveRoot(ctx context.Context, proofSetID uint64, rootID uint64) (res common.Hash, retErr error) {
	log.Infow("removing root", "proofSetID", proofSetID, "rootID", rootID)
	defer func() {
		if retErr != nil {
			log.Errorw("failed to remove root", "proofSetID", proofSetID, "rootID", rootID, "err", retErr)
		} else {
			log.Infow("removed root", "proofSetID", proofSetID, "rootID", rootID, "response", res)
		}
	}()
	// TODO should probably check if we even have the proof set before scheduling a removal

	data, err := p.schedulePieceDeletionsCall(ctx, proofSetID, rootID)
	if err != nil {
		return common.Hash{}, err
	}

	// Prepare the transaction
	ethTx := types.NewTransaction(
		0, // nonce will be set by SenderETH
		p.cfg.Contracts.Verifier,
		big.NewInt(0), // value
		0,             // gas limit (will be estimated)
		nil,           // gas price (will be set by SenderETH)
		data,
	)

	// Send the transaction
	reason := "pdp-delete-root"
	txHash, err := p.sender.Send(ctx, p.address, ethTx, reason)
	if err != nil {
		return common.Hash{}, fmt.Errorf("send transaction: %w", err)
	}

	// Schedule deletion of the root from the proof set using a transaction.
	// Mirror Curio's handleDeleteDataSetPiece: insert message_waits_eth AND
	// set rm_message_hash on the matching pdp_data_set_pieces row.
	txHashLower := strings.ToLower(txHash.Hex())
	comm, err := p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		// Insert into message_waits_eth
		if _, err := tx.Exec(`
			INSERT INTO message_waits_eth (signed_tx_hash, tx_status)
			VALUES ($1, $2)
		`, txHashLower, "pending"); err != nil {
			return false, err
		}

		if _, err := tx.Exec(`
			UPDATE pdp_data_set_pieces
			SET rm_message_hash = $1
			WHERE data_set = $2 AND piece_id = $3
		`, txHashLower, proofSetID, rootID); err != nil {
			return false, err
		}

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return common.Hash{}, fmt.Errorf("scheduling delete root %d from proofset %d: %w", rootID, proofSetID, err)
	}
	if !comm {
		return common.Hash{}, fmt.Errorf("failed to commit remove root")
	}

	return txHash, nil
}

// schedulePieceDeletionsCall packs schedulePieceDeletions for one root, with
// the payer's signature as extraData: FWSS reverts the call with
// ExtraDataRequired without it.
func (p *PDPService) schedulePieceDeletionsCall(ctx context.Context, proofSetID uint64, rootID uint64) ([]byte, error) {
	setID := new(big.Int).SetUint64(proofSetID)
	pieceIDs := []*big.Int{new(big.Int).SetUint64(rootID)}

	dataSet, err := p.serviceContract.GetDataSet(ctx, setID)
	if err != nil {
		return nil, fmt.Errorf("get data set %d: %w", proofSetID, err)
	}
	// The payer signs FWSS's clientDataSetId, not the PDPVerifier set ID.
	signature, err := p.signingService.SignSchedulePieceRemovals(ctx,
		p.id,
		dataSet.ClientDataSetId,
		pieceIDs,
		nil, // proofs: signing service obtains its own access/grant
	)
	if err != nil {
		return nil, fmt.Errorf("sign SchedulePieceRemovals: %w", err)
	}
	extraData, err := p.edc.EncodeSchedulePieceRemovalsExtraData(signature)
	if err != nil {
		return nil, fmt.Errorf("encode extraData: %w", err)
	}

	abiData, err := p.verifierContract.GetABI()
	if err != nil {
		return nil, fmt.Errorf("get contract ABI: %w", err)
	}
	data, err := abiData.Pack("schedulePieceDeletions", setID, pieceIDs, extraData)
	if err != nil {
		return nil, fmt.Errorf("pack ABI method call: %w", err)
	}
	return data, nil
}
