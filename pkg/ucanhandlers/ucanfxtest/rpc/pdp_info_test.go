package rpc_test

import (
	"testing"

	"github.com/fil-forge/libforge/commands/pdp"
	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/server/middleware"
	"github.com/fil-forge/ucantone/ucan/delegation"
	"github.com/fil-forge/ucantone/ucan/invocation"
)

// /pdp/info answers whether a blob has been aggregated yet. It is open about
// who may ask — the handler makes no judgement about the caller — but the ask
// still has to come over this node's authority, which means a delegation the
// node issued. Production grants exactly that: the node delegates /pdp/info to
// the upload service at registration (cmd/cli/setup/register.go).
func (s *RPCSuite) TestPDPInfo_RequiresDelegatedInvocation() {
	t := s.T()
	args := &pdp.InfoArguments{Blob: testutil.RandomMultihash(t)}

	t.Run("a delegated invocation reaches the command", func(t *testing.T) {
		proof := testutil.Must(delegation.Delegate(
			s.ServiceID, s.UploadServiceIdentity.DID(), s.ServiceID.DID(), pdp.Info.Command,
		))(t)
		inv := testutil.Must(pdp.Info.Invoke(
			s.UploadServiceIdentity,
			s.ServiceID.DID(),
			args,
			invocation.WithAudience(s.ServiceID.DID()),
			invocation.WithProofs(proof.Link()),
		))(t)

		// What the command answers is the fake PDP service's business — it
		// declines to compute a commp — so this asserts only that the
		// invocation got past the route's checks.
		assertNotRejected(t, s.sendInvocationWithProofs(t, inv, proof))
	})

	t.Run("a self-signed invocation is refused", func(t *testing.T) {
		stranger := testutil.RandomIssuer(t)
		inv := testutil.Must(pdp.Info.Invoke(
			stranger,
			stranger.DID(),
			args,
			invocation.WithAudience(s.ServiceID.DID()),
		))(t)
		assertReceiptFailure(t, s.sendInvocation(t, inv), middleware.SelfSignedInvocationErrorName)
	})
}
