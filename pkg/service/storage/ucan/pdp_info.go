package ucan

import (
	"bytes"
	"errors"
	"fmt"

	pdpcaps "github.com/fil-forge/libforge/capabilities/pdp"
	ucantone_errors "github.com/fil-forge/ucantone/errors"
	"github.com/fil-forge/ucantone/execution/bindexec"
	"github.com/fil-forge/ucantone/principal"

	"github.com/fil-forge/piri/pkg/pdp"
	"github.com/fil-forge/piri/pkg/service/blobs"
	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/receiptstore"
)

const (
	// PieceNotFoundErrorName surfaces in the receipt failure model when a
	// /pdp/info caller asks about a blob the storage node hasn't seen.
	PieceNotFoundErrorName = "PieceNotFound"
	// AggregationPendingErrorName surfaces when the blob is known but has
	// not yet been aggregated into a piece; the caller is expected to retry.
	AggregationPendingErrorName = "AggregationPending"
)

type PDPInfoService interface {
	ID() principal.Signer
	PDP() pdp.PDP
	Blobs() blobs.Blobs
	Receipts() receiptstore.ReceiptStore
}

// NewPDPInfoHandler returns the /pdp/info handler. It surfaces aggregation
// progress for a blob: when the blob has been accepted and aggregated, the
// /pdp/accept receipt's typed OK fields (Aggregate + Piece + InclusionProof)
// are projected into an InfoOK; when aggregation is still pending, the
// handler returns an AggregationPending failure.
func NewPDPInfoHandler(storageService PDPInfoService) Handler {
	return Handler{
		Capability: pdpcaps.Info,
		Handler: bindexec.NewHandler(func(
			req *bindexec.Request[*pdpcaps.InfoArguments],
			res *bindexec.Response[*pdpcaps.InfoOK],
		) error {
			args := req.Task().Arguments()
			digest := args.Blob

			// Locate the acceptance record for this blob. The same blob may
			// have been accepted into multiple spaces; the PDP-side fields
			// (Piece commitment, aggregate) are identical across spaces, so
			// any acceptance will do.
			acc, err := storageService.Blobs().Acceptances().GetAny(req.Context(), digest)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return res.SetFailure(ucantone_errors.New(
						PieceNotFoundErrorName,
						"no acceptance recorded for blob %s",
						digest,
					))
				}
				return fmt.Errorf("getting acceptance: %w", err)
			}
			if acc.PDPAccept == nil {
				return res.SetFailure(ucantone_errors.New(
					AggregationPendingErrorName,
					"blob %s has not been submitted for PDP aggregation",
					digest,
				))
			}

			// The PDPAccept promise's Link points at the /pdp/accept
			// invocation that was queued during /blob/accept. Look up its
			// receipt; absence means aggregation is still in flight.
			rcpt, err := storageService.Receipts().GetByRan(req.Context(), acc.PDPAccept.UcanAwait.Link)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return res.SetFailure(ucantone_errors.New(
						AggregationPendingErrorName,
						"awaiting /pdp/accept receipt for blob %s",
						digest,
					))
				}
				return fmt.Errorf("looking up /pdp/accept receipt: %w", err)
			}

			out := rcpt.Out()
			if !out.IsOK() {
				_, errBytes := out.Unpack()
				return res.SetFailure(fmt.Errorf("/pdp/accept receipt is a failure: %s", string(errBytes)))
			}
			okBytes, _ := out.Unpack()

			var acceptOK pdpcaps.AcceptOK
			if err := acceptOK.UnmarshalCBOR(bytes.NewReader(okBytes)); err != nil {
				return fmt.Errorf("decoding /pdp/accept receipt OK: %w", err)
			}

			return res.SetSuccess(&pdpcaps.InfoOK{
				Piece: acceptOK.Piece,
				Aggregates: []pdpcaps.InfoAcceptedAggregate{
					{
						Aggregate:      acceptOK.Aggregate,
						InclusionProof: acceptOK.InclusionProof,
					},
				},
			})
		}),
	}
}
