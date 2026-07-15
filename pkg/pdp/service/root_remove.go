package service

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"gorm.io/gorm"

	"github.com/fil-forge/piri/pkg/pdp/service/models"
)

// RemoveRoot schedules the on-chain removal of a root (piece) from a proof
// set: it obtains an eip712 authorization from the signing service, encodes
// it as the schedulePieceDeletions extraData, and submits the transaction.
// The root stops being challenged once the removal takes effect on-chain;
// callers own any local cleanup (bytes, piece refs) and should perform it
// only after the transaction lands.
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

	proofSet := new(big.Int).SetUint64(proofSetID)
	pieceIDs := []*big.Int{new(big.Int).SetUint64(rootID)}

	// Resolve the dataset's client id (and implicitly verify the proof set
	// exists) from the warm-storage service contract — the removal signature
	// is bound to the clientDataSetId, mirroring AddRoots.
	datasetInfo, err := p.serviceContract.GetDataSet(ctx, proofSet)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to get dataset info for proof set %d: %w", proofSetID, err)
	}

	signature, err := p.signingService.SignSchedulePieceRemovals(ctx,
		p.id,
		datasetInfo.ClientDataSetId,
		pieceIDs,
		nil, // proofs (access delegation) — signing-service obtains its own
	)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to sign SchedulePieceRemovals: %w", err)
	}

	extraDataBytes, err := p.edc.EncodeSchedulePieceRemovalsExtraData(signature)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to encode extraData: %w", err)
	}

	// Pack the method call data
	data, err := abiData.Pack("schedulePieceDeletions",
		proofSet,
		pieceIDs,
		extraDataBytes,
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

	// Schedule deletion of the root from the proof set using a transaction
	if err := p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Insert into message_waits_eth
		m := models.MessageWaitsEth{
			SignedTxHash: txHash.String(),
			TxStatus:     "pending",
		}
		tx.WithContext(ctx).Create(&m)
		return nil
	}); err != nil {
		return common.Hash{}, fmt.Errorf("scheduling delete root %d from proofset %d: %w", rootID, proofSetID, err)
	}

	return txHash, nil
}
