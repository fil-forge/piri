package signer

import (
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/execution"

	psclient "github.com/fil-forge/piri-signing-service/pkg/client"
)

// newUpstreamClient builds the remoteUpstream implementation that
// `NewProofServiceSigner` forwards to. It wraps a pre-built ucantone
// HTTPClient in a piri-signing-service psclient via the
// `NewFromHTTPClient` constructor.
func newUpstreamClient(serviceDID did.DID, conn execution.Executor) remoteUpstream {
	return psclient.NewFromHTTPClient(serviceDID, conn)
}
