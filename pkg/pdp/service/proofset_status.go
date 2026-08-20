package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/yugabyte/pgx/v5"

	"github.com/fil-forge/piri/pkg/pdp/types"
)

func (p *PDPService) GetProofSetStatus(ctx context.Context, txHash common.Hash) (res *types.ProofSetStatus, retErr error) {
	log.Infow("getting proof set status", "tx", txHash.String())
	defer func() {
		if retErr != nil {
			log.Errorw("failed to get proof set status", "tx", txHash.String(), "err", retErr)
		} else {
			log.Infow("got proof set status", "tx", txHash.String(), "response", res)
		}
	}()

	txHashLower := strings.ToLower(txHash.Hex())
	var create struct {
		CreateMessageHash string `db:"create_message_hash"`
		Ok                *bool  `db:"ok"`
		DataSetCreated    bool   `db:"data_set_created"`
		Service           string `db:"service"`
	}
	err := p.db.QueryRow(ctx, `
		SELECT create_message_hash, ok, data_set_created, service
		FROM pdp_data_set_creates WHERE create_message_hash = $1
	`, txHashLower).Scan(&create.CreateMessageHash, &create.Ok, &create.DataSetCreated, &create.Service)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, types.NewErrorf(types.KindNotFound, "proof set with transaction hash %s not found", txHash.String())
		}
		return nil, fmt.Errorf("failed to retrieve proof set creation: %w", err)
	}

	if create.Service != p.name {
		return nil, fmt.Errorf("proof set creation not for given service")
	}

	var txStatus string
	err = p.db.QueryRow(ctx, `
		SELECT tx_status FROM message_waits_eth WHERE signed_tx_hash = $1
	`, txHashLower).Scan(&txStatus)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve message wait status: %w", err)
	}

	var id uint64
	if create.DataSetCreated {
		// The data set has been created, get the id from pdp_data_sets
		err = p.db.QueryRow(ctx, `
			SELECT id FROM pdp_data_sets WHERE create_message_hash = $1
		`, txHashLower).Scan(&id)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, fmt.Errorf("proof set not found despite data_set_created = true")
			}
			return nil, fmt.Errorf("failed to retrieve proof set: %w", err)
		}
	}

	return &types.ProofSetStatus{
		TxHash:   common.HexToHash(create.CreateMessageHash),
		TxStatus: txStatus,
		Created:  create.DataSetCreated,
		ID:       id,
	}, nil
}
