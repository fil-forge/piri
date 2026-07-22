package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/fil-forge/filecoin-services/go/eip712"
	"github.com/filecoin-project/curio/harmony/harmonydb"

	"github.com/fil-forge/piri/pkg/pdp/smartcontracts"
)

func (p *PDPService) CreateProofSet(ctx context.Context) (res common.Hash, retErr error) {
	log.Infow("creating proof set")
	defer func() {
		if retErr != nil {
			log.Errorw("failed to create proof set", "error", retErr)
		} else {
			log.Infow("created proof set", "tx", res.String())
		}
	}()

	// Check if the provider is registered
	if err := p.RequireProviderRegistered(ctx); err != nil {
		return common.Hash{}, err
	}

	nonceBytes := make([]byte, 32)
	if _, err := rand.Read(nonceBytes); err != nil {
		return common.Hash{}, fmt.Errorf("failed to generate nonce: %w", err)
	}
	nonce := new(big.Int).SetBytes(nonceBytes)

	var metadataEntries []eip712.MetadataEntry
	// request a signature for creating the dataset from the signing service
	signature, err := p.signingService.SignCreateDataSet(ctx,
		p.id,
		nonce,
		p.address, // Use the nodes address as the address receiving payment for storage
		metadataEntries,
		nil, // proofs: signing service obtains its own access/grant
	)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to sign CreateDataSet: %w", err)
	}

	// Encode the extraData with payer, metadata, and signature
	extraDataBytes, err := p.edc.EncodeCreateDataSetExtraData(
		p.cfg.PayerAddress,
		nonce,
		metadataEntries,
		signature,
	)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to encode extraData: %w", err)
	}

	// Obtain the ABI of the PDPVerifier contract
	abiData, err := p.verifierContract.GetABI()
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to get contract ABI: %w", err)
	}

	// Pack the method call data with listener address and extraData
	data, err := abiData.Pack("createDataSet", p.cfg.Contracts.Service, extraDataBytes)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to pack create proof set: %w", err)
	}

	// Prepare the transaction (nonce will be set to 0, SenderETH will assign it)
	tx := ethtypes.NewTransaction(
		0,
		p.cfg.Contracts.Verifier,
		smartcontracts.SybilFee,
		0,
		nil,
		data,
	)

	reason := "pdp-mkproofset"
	txHash, err := p.sender.Send(ctx, p.address, tx, reason)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to send transaction: %w", err)
	}

	txHashLower := strings.ToLower(txHash.Hex())
	comm, err := p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		n, err := tx.Exec(`
            INSERT INTO message_waits_eth (signed_tx_hash, tx_status)
            VALUES ($1, $2)
        `, txHashLower, "pending")
		if err != nil {
			return false, fmt.Errorf("insert message_waits_eth: %w", err)
		}
		if n != 1 {
			return false, fmt.Errorf("expected 1 row in message_waits_eth, got %d", n)
		}

		n, err = tx.Exec(`
            INSERT INTO pdp_data_set_creates (create_message_hash, service)
            VALUES ($1, $2)
        `, txHashLower, p.name)
		if err != nil {
			return false, fmt.Errorf("insert pdp_data_set_creates: %w", err)
		}
		if n != 1 {
			return false, fmt.Errorf("expected 1 row in pdp_data_set_creates, got %d", n)
		}
		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return common.Hash{}, err
	}
	if !comm {
		return common.Hash{}, fmt.Errorf("failed to commit create data set tracking")
	}

	return txHash, nil
}
