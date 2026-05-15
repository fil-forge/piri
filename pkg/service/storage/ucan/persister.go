package ucan

import (
	"context"
	"fmt"

	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/fil-forge/ucantone/ucan/receipt"
	logging "github.com/ipfs/go-log/v2"

	"github.com/fil-forge/piri/pkg/store/invocationstore"
	"github.com/fil-forge/piri/pkg/store/receiptstore"
)

var persisterLog = logging.Logger("storage/ucan/persister")

// Persister implements ucantone server.RequestDecodeListener and
// ResponseEncodeListener to persist every invocation that arrives at the
// storage UCAN server and every receipt the server emits.
//
// Capturing both sides is what lets `getAddPieceProofs` (and any other
// signing-service-style flow) bundle the (invocation, receipt) pair for a
// previously-executed `/blob/accept` or `/pdp/accept` invocation without
// per-handler bookkeeping.
type Persister struct {
	Invocations invocationstore.InvocationStore
	Receipts    receiptstore.ReceiptStore
}

// OnRequestDecode persists every invocation in the decoded inbound
// container. Errors are logged but never returned — failing to persist a
// proof block should not break the active request.
func (p *Persister) OnRequestDecode(ctx context.Context, ct ucan.Container) error {
	for _, inv := range ct.Invocations() {
		typed, err := invocation.Decode(inv.Bytes())
		if err != nil {
			persisterLog.Warnw("decoding inbound invocation", "error", err)
			continue
		}
		if err := p.Invocations.Put(ctx, typed); err != nil {
			persisterLog.Warnw("persisting inbound invocation",
				"invocation", typed.Link(), "error", err)
		}
	}
	return nil
}

// OnResponseEncode persists every receipt in the outbound container before
// it is encoded to the wire. Storing here (rather than in each handler)
// keeps the per-handler code free of bookkeeping concerns.
func (p *Persister) OnResponseEncode(ctx context.Context, ct ucan.Container) error {
	for _, r := range ct.Receipts() {
		typed, err := receipt.Decode(r.Bytes())
		if err != nil {
			return fmt.Errorf("decoding outbound receipt: %w", err)
		}
		if err := p.Receipts.Put(ctx, typed); err != nil {
			persisterLog.Warnw("persisting outbound receipt",
				"receipt", typed.Link(), "ran", typed.Ran(), "error", err)
		}
	}
	return nil
}
