package chainrpc

import "github.com/filecoin-project/curio/lib/ethchain"

//go:generate go run ./internal/gen

// EthClient is Curio's ethchain.EthClient with every call that returns an
// error recorded in Metrics under its Go method name. Its methods are
// generated, one per method of the interface, so none can bypass the record.
type EthClient struct {
	next    ethchain.EthClient
	metrics *Metrics
}

// The generated methods must cover the whole interface: this fails to compile
// after Curio adds a method, until go generate is run again.
var _ ethchain.EthClient = (*EthClient)(nil)

// NewEthClient wraps next so its calls are recorded in metrics. A nil metrics
// records nothing.
func NewEthClient(next ethchain.EthClient, metrics *Metrics) *EthClient {
	return &EthClient{next: next, metrics: metrics}
}
