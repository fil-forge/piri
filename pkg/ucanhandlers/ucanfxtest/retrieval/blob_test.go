package retrieval_test

import (
	"io"
	"net/http"
	"strconv"

	"github.com/fil-forge/libforge/commands/blob"
	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/libforge/ucan/retrieval"
	"github.com/fil-forge/ucantone/execution"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/ipfs/go-cid"
	mh "github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"
)

// TestBlobRetrieve_Basic exercises /blob/retrieve over the
// header-container byte-streaming transport. The service signs (issuer
// equals subject equals audience), so no proof chain is needed — the
// focus here is the read + HTTP-shape pipeline that
// pkg/ucanhandlers/blob.Retrieve drives.
func (s *RetrievalSuite) TestBlobRetrieve_Basic() {
	t := s.T()

	data := testutil.RandomBytes(t, 256)
	digest, err := mh.Sum(data, mh.SHA2_256, -1)
	require.NoError(t, err)
	s.Pieces.Put(digest, data)

	inv, err := blob.Retrieve.Invoke(
		s.ServiceID,
		s.ServiceID.DID(),
		&blob.RetrieveArguments{Blob: blob.RetrieveBlob{Digest: digest}},
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

	require.Equal(t, http.StatusOK, container.StatusCode, "full-blob read returns 200")
	require.Equal(t, strconv.Itoa(len(data)), container.Header.Get("Content-Length"))

	body, err := io.ReadAll(container.Body)
	require.NoError(t, err)
	require.Equal(t, data, body, "body matches seeded bytes")

	require.True(t, resp.Receipt().Out().IsOK(), "receipt mirrors HTTP success")
}
