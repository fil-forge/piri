package manager

import (
	"context"
	"fmt"

	libforge_pdp "github.com/fil-forge/libforge/capabilities/pdp"
	libforge_merkletree "github.com/fil-forge/libforge/merkletree"
	"github.com/fil-forge/ucantone/principal"
	"github.com/fil-forge/ucantone/ucan/invocation"
	ucantone_receipt "github.com/fil-forge/ucantone/ucan/receipt"
	"github.com/ipfs/go-cid"

	"github.com/fil-forge/piri/pkg/pdp/aggregation/aatodo_types"
	"github.com/fil-forge/piri/pkg/pdp/aggregation/types"
	apitypes "github.com/fil-forge/piri/pkg/pdp/types"
	"github.com/fil-forge/piri/pkg/store/receiptstore"
)

type PieceAcceptor struct {
	issuer         principal.Signer
	aggregateStore types.Store
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
	aggregates := make([]aatodo_types.Aggregate, 0, len(aggregateLinks))
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
	for _, rcpt := range receipts {
		if err := pa.receiptStore.Put(ctx, rcpt); err != nil {
			return err
		}
	}
	return nil
}

func GenerateReceipts(ctx context.Context, issuer principal.Signer, aggregate aatodo_types.Aggregate, resolver apitypes.PieceResolverAPI) ([]*ucantone_receipt.Receipt, error) {
	receipts := make([]*ucantone_receipt.Receipt, 0, len(aggregate.Pieces))
	for _, aggregatePiece := range aggregate.Pieces {
		pieceCID := aggregatePiece.Link.Link()
		blob, found, err := resolver.ResolveToBlob(ctx, pieceCID.Hash())
		if err != nil {
			return nil, fmt.Errorf("resolving piece for receipt: %w", err)
		}
		if !found {
			return nil, fmt.Errorf("piece not found for receipt generation: %s", pieceCID)
		}
		inv, err := libforge_pdp.Accept.Invoke(
			issuer,
			issuer.DID(),
			&libforge_pdp.AcceptArguments{Blob: blob},
			invocation.WithNoExpiration(),
		)
		if err != nil {
			return nil, fmt.Errorf("generating invocation: %w", err)
		}

		proof := libforge_merkletree.ProofData{
			Path:  make([][]byte, len(aggregatePiece.InclusionProof.Path)),
			Index: aggregatePiece.InclusionProof.Index,
		}
		for i, n := range aggregatePiece.InclusionProof.Path {
			b := make([]byte, len(n))
			copy(b, n[:])
			proof.Path[i] = b
		}

		ok := &libforge_pdp.AcceptOK{
			Aggregate:      aggregate.Root.Link(),
			InclusionProof: proof,
			Piece:          pieceCID,
		}
		rcpt, err := ucantone_receipt.IssueOK(issuer, inv.Link(), ok)
		if err != nil {
			return nil, fmt.Errorf("issuing receipt: %w", err)
		}
		receipts = append(receipts, rcpt)
	}
	return receipts, nil
}

func GenerateReceiptsForAggregates(ctx context.Context, issuer principal.Signer, aggregates []aatodo_types.Aggregate, resolver apitypes.PieceResolverAPI) ([]*ucantone_receipt.Receipt, error) {
	size := 0
	for _, aggregate := range aggregates {
		size += len(aggregate.Pieces)
	}
	receipts := make([]*ucantone_receipt.Receipt, 0, size)
	for _, aggregate := range aggregates {
		aggregateReceipts, err := GenerateReceipts(ctx, issuer, aggregate, resolver)
		if err != nil {
			return nil, err
		}
		receipts = append(receipts, aggregateReceipts...)
	}
	return receipts, nil
}
