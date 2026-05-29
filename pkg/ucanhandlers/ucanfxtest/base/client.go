package base

import (
	"testing"

	"github.com/fil-forge/libforge/ucan/retrieval"
	"github.com/fil-forge/ucantone/client"
	"github.com/stretchr/testify/require"
)

// RPCClient returns a ucantone HTTP client pointed at the suite's
// body-CAR RPC endpoint (POST /). Use this for capabilities whose
// response is itself a UCAN container of receipts: blob/accept,
// blob/allocate, access/grant, pdp/info, etc.
func (s *BaseSuite) RPCClient(t *testing.T) *client.HTTPClient {
	t.Helper()
	c, err := client.NewHTTP(s.ServiceURL)
	require.NoError(t, err)
	return c
}

// RetrievalClient returns a libforge retrieval client pointed at the
// suite's byte-streaming endpoint (GET /piece/:cid). Use this for
// capabilities whose response body is the raw blob bytes:
// blob/retrieve, content/retrieve.
//
// The path argument selects the route (e.g. "/piece/<cid>"). Callers
// resolve the actual CID per-invocation because the byte-streaming
// transport encodes the target in the URL path.
func (s *BaseSuite) RetrievalClient(t *testing.T, path string) *retrieval.HTTPHeaderClient {
	t.Helper()
	c, err := retrieval.NewClient(s.ResolveURL(path))
	require.NoError(t, err)
	return c
}
