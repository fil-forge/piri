package blob

import (
	"context"
	stderrors "errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/fil-forge/libforge/commands/blob"
	"github.com/fil-forge/libforge/ucan/retrieval"
	"github.com/fil-forge/ucantone/binding"
	"github.com/fil-forge/ucantone/errors"
	"github.com/fil-forge/ucantone/principal"
	"github.com/fil-forge/ucantone/server"
	"github.com/fil-forge/ucantone/ucan/container"
	"github.com/multiformats/go-multihash"
	fxlib "go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/pdp/types"
	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/blobstore"
)

// InvalidResourceErrorName is the name given to an error where the resource did
// not match the service DID.
const InvalidResourceErrorName = "InvalidResource"

// HTTP header carrying the stable error name on non-2xx responses, so
// test code (and any byte-streaming client) can match on the failure
// category without parsing the body. The Body still holds the human-
// readable message.
const ErrorNameHeader = "X-Piri-Error-Name"

// Stable error names attached to non-2xx responses via [ErrorNameHeader]
// and returned as the typed receipt failure from [Retrieve].
const (
	NotFoundErrorName            = "NotFound"
	RangeNotSatisfiableErrorName = "RangeNotSatisfiable"
	InternalServerErrorName      = "InternalServerError"
)

// BlobRetrieveDeps is the dependency set populated by fx for the
// blob/retrieve UCAN method.
type BlobRetrieveDeps struct {
	fxlib.In
	ID     principal.Signer
	Pieces types.PieceReaderAPI
}

func NewBlobRetrieveHandler(deps BlobRetrieveDeps) server.Route {
	return server.NewRoute(
		blob.Retrieve,
		func(req *binding.Request[*blob.RetrieveArguments], rsp *binding.Response[*blob.RetrieveOK]) error {
			args := req.Task().Arguments()

			// /blob/retrieve is service-level: no space scoping, no byte range.
			container, derr := Retrieve(req.Context(), deps.Pieces, args.Blob.Digest, nil)
			if err := rsp.SetMetadata(container); err != nil {
				return err
			}
			if derr != nil {
				return rsp.SetFailure(derr)
			}
			return rsp.SetSuccess(&blob.RetrieveOK{})
		},
	)
}

// Retrieve reads a blob (or a range of it) from the piece store and
// returns a retrieval response container ready to be set as
// rsp.SetMetadata on a binding response.
//
// On success the container carries a 2xx status with the byte stream
// and a nil error. On a known read failure the container carries the
// appropriate non-2xx status, a stable error name in [ErrorNameHeader],
// and a human-readable body; the returned error is the typed UCAN
// failure (named) that the caller should pass to rsp.SetFailure so the
// receipt mirrors the HTTP outcome.
//
// Both /blob/retrieve (service-level) and /space/content/retrieve
// (space-scoped, ranged) wrap this the same way:
//
//	container, derr := blob.Retrieve(ctx, pieces, digest, byteRange)
//	if err := rsp.SetMetadata(container); err != nil { return err }
//	if derr != nil { return rsp.SetFailure(derr) }
//	return rsp.SetSuccess(&blob.RetrieveOK{})   // or &content.RetrieveOK{}
//
// Unrecognized read failures map to a 500 container and an
// InternalServerError-named error; the function never returns
// (nil, _) — callers can always SetMetadata first.
func Retrieve(
	ctx context.Context,
	pieces types.PieceReaderAPI,
	digest multihash.Multihash,
	byteRange *blobstore.Range,
) (*retrieval.HTTPHeaderResponseContainer, error) {
	var readOpts []types.ReadPieceOption
	if byteRange != nil && (byteRange.Start > 0 || byteRange.End != nil) {
		readOpts = append(readOpts, types.WithRange(byteRange.Start, byteRange.End))
	}

	piece, err := pieces.Read(ctx, digest, readOpts...)
	if err != nil {
		var rangeErr blobstore.RangeNotSatisfiableError
		switch {
		case stderrors.Is(err, store.ErrNotFound):
			log.Debugw("blob not found", "digest", digest.B58String())
			return errorResponse(
				http.StatusNotFound,
				NotFoundErrorName,
				fmt.Sprintf("blob not found: %s", digest.B58String()),
			)
		case stderrors.As(err, &rangeErr):
			log.Debugw("range not satisfiable", "digest", digest.B58String(), "range", rangeErr.Error())
			return errorResponse(
				http.StatusRequestedRangeNotSatisfiable,
				RangeNotSatisfiableErrorName,
				rangeErr.Error(),
			)
		default:
			log.Errorw("reading piece", "error", err, "digest", digest.B58String())
			return errorResponse(
				http.StatusInternalServerError,
				InternalServerErrorName,
				err.Error(),
			)
		}
	}

	digestStr := digest.B58String()
	status := http.StatusOK
	headers := http.Header{}
	contentLength := uint64(piece.Size)

	if byteRange != nil {
		start := byteRange.Start
		end := uint64(piece.Size - 1)
		if byteRange.End != nil {
			end = *byteRange.End
		}
		contentLength = end - start + 1

		if contentLength != uint64(piece.Size) {
			status = http.StatusPartialContent
			headers.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, piece.Size))
			headers.Add("Vary", "Range")
		}
	}

	headers.Set("Content-Length", fmt.Sprintf("%d", contentLength))
	headers.Set("Content-Type", "application/octet-stream")
	headers.Set("Cache-Control", "public, max-age=29030400, immutable")
	headers.Set("Etag", fmt.Sprintf(`"%s"`, digestStr))
	headers.Add("Vary", "Accept-Encoding")

	log.Debugw("serving bytes", "status", status, "size", contentLength, "digest", digestStr)

	return &retrieval.HTTPHeaderResponseContainer{
		Container:  container.New(),
		StatusCode: status,
		Header:     headers,
		Body:       piece.Data,
	}, nil
}

// errorResponse builds the (container, error) pair for a domain failure.
// The container carries the HTTP shape (status + name header + message
// body); the returned error is the typed UCAN failure suitable for
// rsp.SetFailure so the receipt outcome mirrors the HTTP status.
func errorResponse(status int, errorName, message string) (*retrieval.HTTPHeaderResponseContainer, error) {
	headers := http.Header{}
	headers.Set(ErrorNameHeader, errorName)
	headers.Set("Content-Type", "text/plain; charset=utf-8")
	headers.Set("Content-Length", fmt.Sprintf("%d", len(message)))
	return &retrieval.HTTPHeaderResponseContainer{
		Container:  container.New(),
		StatusCode: status,
		Header:     headers,
		Body:       io.NopCloser(strings.NewReader(message)),
	}, errors.New(errorName, "%s", message)
}
