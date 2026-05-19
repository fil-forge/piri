package manager

import (
	"context"
	"fmt"

	// TODO(forrest)[ucan1]: trash. remove this dep
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"

	"github.com/fil-forge/libforge/capabilities/pdp"
	"github.com/fil-forge/ucantone/principal"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/fil-forge/ucantone/ucan/receipt"
	"github.com/ipfs/go-cid"

	"github.com/fil-forge/piri/internal/ipldstore"
	"github.com/fil-forge/piri/pkg/pdp/aggregation/types"
	apitypes "github.com/fil-forge/piri/pkg/pdp/types"
	"github.com/fil-forge/piri/pkg/store/receiptstore"
)

type PieceAcceptor struct {
	issuer         principal.Signer
	aggregateStore ipldstore.KVStore[cid.Cid, types.Aggregate]
	receiptStore   receiptstore.ReceiptStore
	resolver       apitypes.PieceResolverAPI
}

func NewPieceAccepter(issuer principal.Signer, aggregateStore types.Store, receiptStore receiptstore.ReceiptStore, resolver apitypes.PieceResolverAPI) *PieceAcceptor {
	return &PieceAcceptor{
		issuer:         issuer,
		aggregateStore: aggregateStore,
		receiptStore:   receiptStore,
		resolver:       resolver,
	}
}

func (pa *PieceAcceptor) AcceptPieces(ctx context.Context, aggregateLinks []cid.Cid) error {
	// TODO we can run this in parallel since receipt generation requires resolving pdp pieces in links to blobs
	aggregates := make([]types.Aggregate, 0, len(aggregateLinks))
	for _, aggregateLink := range aggregateLinks {
		aggregate, err := pa.aggregateStore.Get(ctx, aggregateLink)
		if err != nil {
			return fmt.Errorf("reading aggregates: %w", err)
		}
		aggregates = append(aggregates, aggregate)
	}
	// TODO: Should we actually send a piece accept invocation? It seems unnecessary it's all the same machine
	receipts, err := GenerateReceiptsForAggregates(ctx, pa.issuer, aggregates, pa.resolver)
	if err != nil {
		return fmt.Errorf("generating receipts: %w", err)
	}
	for _, receipt := range receipts {
		if err := pa.receiptStore.Put(ctx, receipt); err != nil {
			return err
		}
	}
	return nil
}

func GenerateReceipts(ctx context.Context, issuer ucan.Signer, aggregate types.Aggregate, resolver apitypes.PieceResolverAPI) ([]*receipt.Receipt, error) {
	receipts := make([]*receipt.Receipt, 0, len(aggregate.Pieces))
	for _, aggregatePiece := range aggregate.Pieces {
		blob, found, err := resolver.ResolveToBlob(ctx, aggregatePiece.Link.Link().(cidlink.Link).Cid.Hash())
		if err != nil {
			return nil, fmt.Errorf("resolving piece for receipt: %w", err)
		}
		if !found {
			return nil, fmt.Errorf("piece not found for receipt generation: %s", aggregatePiece.Link.Link().String())
		}
		inv, err := pdp.Accept.Invoke(
			issuer,
			issuer.DID(),
			&pdp.AcceptArguments{
				Blob: blob,
			},
			invocation.WithNoExpiration(),
		)

		if err != nil {
			return nil, fmt.Errorf("generating invocation: %w", err)
		}
		// TODO(forrest)[ucan1] fix ipld garbage/implement new a PieceLink type.
		rcpt, err := receipt.IssueOK(issuer, inv.Link(), &pdp.AcceptOK{
			Aggregate:      aggregate.Root.Link().(cidlink.Link).Cid,
			InclusionProof: aggregatePiece.InclusionProof,
			Piece:          aggregatePiece.Link.Link().(cidlink.Link).Cid,
		})
		if err != nil {
			return nil, fmt.Errorf("issuing receipt: %w", err)
		}
		receipts = append(receipts, rcpt)
	}
	return receipts, nil
}

func GenerateReceiptsForAggregates(ctx context.Context, issuer ucan.Signer, aggregates []types.Aggregate, resolver apitypes.PieceResolverAPI) ([]*receipt.Receipt, error) {
	size := 0
	for _, aggregate := range aggregates {
		size += len(aggregate.Pieces)
	}
	receipts := make([]*receipt.Receipt, 0, size)
	for _, aggregate := range aggregates {
		aggregateReceipts, err := GenerateReceipts(ctx, issuer, aggregate, resolver)
		if err != nil {
			return nil, err
		}
		receipts = append(receipts, aggregateReceipts...)
	}
	return receipts, nil
}
