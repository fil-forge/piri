package retrieval_test

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/fil-forge/libforge/commands/blob"
	contentcap "github.com/fil-forge/libforge/commands/content"
	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/libforge/ucan/retrieval"
	"github.com/fil-forge/ucantone/errors/datamodel"
	"github.com/fil-forge/ucantone/execution"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/ipfs/go-cid"
	mh "github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/store/allocationstore/allocation"
	contenthandler "github.com/fil-forge/piri/pkg/ucanhandlers/content"
)

// /space/content/retrieve is space-scoped: the invocation subject is
// the space, the handler gates on the per-space allocation, and a
// successful read streams bytes back through the HTTP response. Tests
// here cover (a) the happy ranged-read path and (b) the missing-
// allocation rejection.

func (s *RetrievalSuite) TestContentRetrieve_Basic() {
	t := s.T()

	space := testutil.RandomIssuer(t)
	data := testutil.RandomBytes(t, 256)
	digest, err := mh.Sum(data, mh.SHA2_256, -1)
	require.NoError(t, err)

	require.NoError(t, s.Allocations.Put(t.Context(), allocation.Allocation{
		Space:   space.DID(),
		Blob:    blob.Blob{Digest: digest, Size: uint64(len(data))},
		Expires: ucan.UnixTimestamp(time.Now().Add(time.Hour).Unix()),
		Cause:   testutil.RandomCID(t),
	}))
	s.Pieces.Put(digest, data)

	inv, err := contentcap.Retrieve.Invoke(
		space,
		space.DID(),
		&contentcap.RetrieveArguments{
			Blob:  contentcap.Blob{Digest: digest},
			Range: contentcap.Range{Start: 0, End: 1},
		},
		// Audience is the service so the retrieval server accepts the
		// invocation; subject stays as the space because the content
		// handler reads space from req.Task().Subject().
		invocation.WithAudience(s.ServiceID.DID()),
	)
	require.NoError(t, err)

	pieceCID := cid.NewCidV1(cid.Raw, digest)
	client, err := retrieval.NewClient(s.BaseSuite.ResolveURL("/piece/" + pieceCID.String()))
	require.NoError(t, err)

	resp, err := client.Execute(execution.NewRequest(t.Context(), inv))
	require.NoError(t, err)

	container, ok := resp.Metadata().(*retrieval.HTTPHeaderResponseContainer)
	require.True(t, ok, "response metadata should be a *HTTPHeaderResponseContainer; got %T", resp.Metadata())
	defer container.Body.Close()

	require.Equal(t, http.StatusPartialContent, container.StatusCode,
		"a sub-range read returns 206 Partial Content")
	require.Equal(t, "2", container.Header.Get("Content-Length"),
		"Content-Length matches range size (end - start + 1)")
	require.Equal(t, "bytes 0-1/"+strconv.Itoa(len(data)), container.Header.Get("Content-Range"))

	body, err := io.ReadAll(container.Body)
	require.NoError(t, err)
	require.Equal(t, data[:2], body, "body is the requested byte range")

	require.True(t, resp.Receipt().Out().IsOK(), "receipt mirrors HTTP success")
}

func (s *RetrievalSuite) TestContentRetrieve_NotAllocated() {
	t := s.T()

	space := testutil.RandomIssuer(t)
	digest := testutil.RandomMultihash(t)
	// intentionally no allocation in s.Allocations and no bytes in s.Pieces

	inv, err := contentcap.Retrieve.Invoke(
		space,
		space.DID(),
		&contentcap.RetrieveArguments{
			Blob:  contentcap.Blob{Digest: digest},
			Range: contentcap.Range{Start: 0, End: 1},
		},
		// Audience is the service so the retrieval server accepts the
		// invocation; subject stays as the space because the content
		// handler reads space from req.Task().Subject().
		invocation.WithAudience(s.ServiceID.DID()),
	)
	require.NoError(t, err)

	pieceCID := cid.NewCidV1(cid.Raw, digest)
	client, err := retrieval.NewClient(s.BaseSuite.ResolveURL("/piece/" + pieceCID.String()))
	require.NoError(t, err)

	resp, err := client.Execute(execution.NewRequest(t.Context(), inv))
	require.NoError(t, err)

	rcpt := resp.Receipt()
	require.True(t, rcpt.Out().IsErr(),
		"missing allocation must fail the receipt, not just the byte stream")

	_, errBytes := rcpt.Out().Unpack()
	var em datamodel.ErrorModel
	require.NoError(t, em.UnmarshalCBOR(bytes.NewReader(errBytes)), "decoding error model")
	require.Equal(t, contenthandler.NotAllocatedErrorName, em.ErrorName,
		"handler raised NotAllocated for unallocated space")
}
