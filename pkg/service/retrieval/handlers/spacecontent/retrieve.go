package spacecontent

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/fil-forge/libforge/digestutil"
	ucantone_errors "github.com/fil-forge/ucantone/errors"
	"github.com/fil-forge/libforge/ucan/retrieval"
	logging "github.com/ipfs/go-log/v2"
	"github.com/multiformats/go-multihash"

	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/blobstore"
)

var log = logging.Logger("retrieval/handlers/spacecontent")

// Stable error names surfaced in receipt failure models.
const (
	NotFoundErrorName             = "NotFound"
	RangeNotSatisfiableErrorName  = "RangeNotSatisfiable"
)

// Retrieve resolves a blob from the local blob store and packages the bytes
// into an HTTPHeaderResponseContainer suitable for [execution.Response.SetMetadata].
//
// On success, the returned container has status 200 (or 206 for a partial
// range), Content-Length/Etag/Content-Type/Cache-Control headers, and the
// blob bytes in Body.
//
// On expected misses (blob absent, range not satisfiable) the returned error
// is a [ucantone/errors.Named] with a stable name so the caller can pass it
// straight to res.SetFailure and get a well-named failure receipt; the
// container in that case carries the matching HTTP status and an empty body.
func Retrieve(
	ctx context.Context,
	blobs blobstore.BlobGetter,
	digest multihash.Multihash,
	byteRange *blobstore.Range,
) (*retrieval.HTTPHeaderResponseContainer, error) {
	digestStr := digestutil.Format(digest)

	var getOpts []blobstore.GetOption
	if byteRange != nil {
		start := byteRange.Start
		end := byteRange.End
		if start > 0 || end != nil {
			getOpts = append(getOpts, blobstore.WithRange(start, end))
		}
	}

	blob, err := blobs.Get(ctx, digest, getOpts...)
	if err != nil {
		var erns blobstore.RangeNotSatisfiableError
		if errors.Is(err, store.ErrNotFound) {
			log.Debugw("blob not found", "digest", digestStr)
			return &retrieval.HTTPHeaderResponseContainer{
				StatusCode: http.StatusNotFound,
				Header:     http.Header{},
			}, ucantone_errors.New(NotFoundErrorName, "blob not found: %s", digestStr)
		}
		if errors.As(err, &erns) {
			log.Debugw("range not satisfiable", "digest", digestStr)
			return &retrieval.HTTPHeaderResponseContainer{
				StatusCode: http.StatusRequestedRangeNotSatisfiable,
				Header:     http.Header{},
			}, ucantone_errors.New(RangeNotSatisfiableErrorName, "%s", erns.Error())
		}
		log.Errorw("getting blob", "error", err)
		return nil, fmt.Errorf("getting blob: %w", err)
	}

	status := http.StatusOK
	headers := http.Header{}
	contentLength := uint64(blob.Size())

	if byteRange != nil {
		start := byteRange.Start
		end := uint64(blob.Size() - 1) // inclusive
		if byteRange.End != nil {
			end = *byteRange.End
		}
		contentLength = end - start + 1
		if contentLength != uint64(blob.Size()) {
			status = http.StatusPartialContent
			headers.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, blob.Size()))
			headers.Add("Vary", "Range")
		}
	}

	headers.Set("Content-Length", fmt.Sprintf("%d", contentLength))
	headers.Set("Content-Type", "application/octet-stream")
	headers.Set("Cache-Control", "public, max-age=29030400, immutable")
	headers.Set("Etag", fmt.Sprintf(`"%s"`, digestStr))
	headers.Set("Vary", "Accept-Encoding")

	log.Debugw("serving bytes", "status", status, "size", contentLength, "digest", digestStr)

	return &retrieval.HTTPHeaderResponseContainer{
		StatusCode: status,
		Header:     headers,
		Body:       blob.Body(),
	}, nil
}
