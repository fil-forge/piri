package rpc_test

import (
	"net/url"
	"testing"

	"github.com/fil-forge/libforge/identity"
	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/did/key"
	"github.com/fil-forge/ucantone/did/resolver"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/stretchr/testify/suite"
	"go.uber.org/fx"

	piritestutil "github.com/fil-forge/piri/pkg/internal/testutil"
	"github.com/fil-forge/piri/pkg/internal/testutil/pdpfake"
	"github.com/fil-forge/piri/pkg/service/publisher"
	"github.com/fil-forge/piri/pkg/store/acceptancestore"
	"github.com/fil-forge/piri/pkg/store/allocationstore"
	"github.com/fil-forge/piri/pkg/store/invocationstore"
	"github.com/fil-forge/piri/pkg/ucanhandlers/ucanfxtest/base"
)

// RPCSuite covers capabilities served on the body-CAR RPC transport
// (POST /): blob/accept, blob/allocate, access/grant, pdp/info, etc.
//
// Test methods live in sibling files (access_grant_test.go,
// blob_allocate_test.go, blob_accept_test.go) and attach to *RPCSuite
// so they share the fx app stood up here. Stores the handlers write to
// are populated as suite fields for in-process assertions.
type RPCSuite struct {
	base.BaseSuite

	UploadServiceIdentity identity.Identity
	UploadServiceURL      *url.URL

	// Stores + side-effect surfaces tests inspect.
	Allocations allocationstore.AllocationStore
	Acceptances acceptancestore.AcceptanceStore
	ClaimStore  invocationstore.InvocationStore
	Pieces      *pdpfake.Pieces
	Publisher   publisher.Publisher
}

func (s *RPCSuite) SetupSuite() {
	s.ServiceID = testutil.Alice
	s.UploadServiceIdentity = identity.Identity{Issuer: testutil.WebService}
	s.UploadServiceURL = testutil.TestURL

	s.ConfigOptions = []piritestutil.TestConfigOption{
		piritestutil.WithUploadServiceConfig(s.UploadServiceIdentity.DID(), s.UploadServiceURL),
	}

	// We support resolving exactly one "did:web": the upload service
	uploadServiceDoc, err := identity.Identity{Issuer: s.UploadServiceIdentity}.DIDDocument()
	s.Require().NoError(err)

	s.ExtraOptions = []fx.Option{
		// Swap the production HTTP/cached resolver for local resolution.
		// did:key DIDs encode their public key directly so we decode in
		// process — no network, works for any test signer (Alice, Bob,
		// Mallory, etc.). did:web still needs the map for WebService.
		fx.Decorate(func(did.Resolver) did.Resolver {
			return resolver.ByMethod{
				"key": key.Resolver,
				"web": resolver.WellKnown{
					s.UploadServiceIdentity.DID(): uploadServiceDoc,
				},
			}
		}),
		fx.Populate(
			&s.Allocations,
			&s.Acceptances,
			&s.ClaimStore,
			&s.Pieces,
			&s.Publisher,
		),
	}
	s.BaseSuite.SetupSuite()
}

func TestRPCSuite(t *testing.T) {
	suite.Run(t, new(RPCSuite))
}

// didWrappedVerifier overrides the wrapped verifier's DID() with the
// resolver's input DID, preserving the underlying signature check.
type didWrappedVerifier struct {
	did   did.DID
	inner ucan.Verifier
}

func (w *didWrappedVerifier) DID() did.DID                { return w.did }
func (w *didWrappedVerifier) Verify(msg, sig []byte) bool { return w.inner.Verify(msg, sig) }
