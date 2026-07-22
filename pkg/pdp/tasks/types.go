// Package tasks retains the eth-client interface contracts and constants used by
// the PDP service. The task IMPLEMENTATIONS (SenderETH, watchers, proving tasks)
// were replaced by Curio's pdpv0 pipeline (harmonytask + tasks/message); only
// these small contracts remain, satisfied by Piri's eth client.
package tasks

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

// MinConfidence is the number of confirmations to wait before treating an
// on-chain transaction as final.
const MinConfidence = 6

// SenderETHClient is the eth-client surface needed to submit transactions.
type SenderETHClient interface {
	NetworkID(ctx context.Context) (*big.Int, error)
	HeaderByNumber(ctx context.Context, number *big.Int) (*ethtypes.Header, error)
	PendingNonceAt(ctx context.Context, account common.Address) (uint64, error)
	EstimateGas(ctx context.Context, msg ethereum.CallMsg) (uint64, error)
	SendTransaction(ctx context.Context, transaction *ethtypes.Transaction) error
	SuggestGasTipCap(ctx context.Context) (*big.Int, error)
}

// MessageWatcherEthClient is the eth-client surface needed to watch tx status.
type MessageWatcherEthClient interface {
	TransactionByHash(ctx context.Context, hash common.Hash) (tx *ethtypes.Transaction, isPending bool, err error)
	TransactionReceipt(ctx context.Context, txHash common.Hash) (*ethtypes.Receipt, error)
}
