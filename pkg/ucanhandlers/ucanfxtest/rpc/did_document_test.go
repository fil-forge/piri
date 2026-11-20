package rpc_test

import (
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/did/web"
	"github.com/fil-forge/ucantone/multikey"
)

// nodeResolver resolves did:web the way another service does: by fetching the
// DID document from the host the DID names. The test node serves plain HTTP.
func (s *RPCSuite) nodeResolver() did.Resolver {
	r, err := web.NewResolver(web.WithInsecure(true))
	s.Require().NoError(err)
	return r
}

// The node is identified by the did:web of its public URL, and serves the DID
// document that resolves to: its DID, and the key it signs with.
func (s *RPCSuite) TestDIDDocument() {
	t := s.T()
	s.Require().Equal(web.Method, s.ServiceID.DID().Method())

	doc, err := s.nodeResolver().Resolve(s.Ctx(), s.ServiceID.DID())
	s.Require().NoError(err)
	s.Require().Equal(s.ServiceID.DID(), doc.ID)

	vms := doc.CapabilityInvocation.All()
	s.Require().Len(vms, 1)
	verifier, err := multikey.DeriveVerifier(t.Context(), vms[0].Material)
	s.Require().NoError(err)

	msg := []byte("signed by the node")
	s.Require().True(verifier.Verify(msg, s.ServiceID.Sign(msg)), "the document publishes the node's signing key")
}
