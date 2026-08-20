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
	// Get the ABI and pack the transaction data
	abiData, err := p.verifierContract.GetABI()
	if err != nil {
		return common.Hash{}, fmt.Errorf("get contract ABI: %w", err)
	}

	// TODO should probably check if we even have the proof set before scheduling a removal

	// TODO this will surely fail without extraData as a signature.
	// Pack the method call data
	data, err := abiData.Pack("schedulePieceDeletions",
		big.NewInt(int64(proofSetID)),
		[]*big.Int{big.NewInt(int64(rootID))},
		[]byte{},
	)
	if err != nil {
		return common.Hash{}, fmt.Errorf("pack ABI method call: %w", err)
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
