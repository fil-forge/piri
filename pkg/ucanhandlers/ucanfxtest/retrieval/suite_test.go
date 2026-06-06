package retrieval_test

import (
	"net/url"
	"testing"

	"github.com/fil-forge/libforge/identity"
	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/did/key"
	"github.com/fil-forge/ucantone/did/resolver"
	"github.com/stretchr/testify/suite"
	"go.uber.org/fx"

	piritestutil "github.com/fil-forge/piri/pkg/internal/testutil"
	"github.com/fil-forge/piri/pkg/internal/testutil/pdpfake"
	"github.com/fil-forge/piri/pkg/store/allocationstore"
	"github.com/fil-forge/piri/pkg/ucanhandlers/ucanfxtest/base"
)

// RetrievalSuite covers capabilities served on the header-container
// byte-streaming transport (GET /piece/:cid): blob/retrieve,
// content/retrieve.
//
// Test methods are split across sibling files in this package
// (blob_test.go, content_test.go) but all attach to *RetrievalSuite so
// they share the fx app stood up in SetupSuite.
type RetrievalSuite struct {
	base.BaseSuite

	Allocations allocationstore.AllocationStore
	Pieces      *pdpfake.Pieces

	UploadServiceIdentity identity.Identity
	UploadServiceURL      *url.URL
}

func TestRetrievalSuite(t *testing.T) {
	suite.Run(t, new(RetrievalSuite))
}

func (s *RetrievalSuite) SetupSuite() {
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
		fx.Decorate(func(did.Resolver) did.Resolver {
			return resolver.ByMethod{
				"key": key.Resolver,
				"web": resolver.WellKnown{
					s.UploadServiceIdentity.DID(): uploadServiceDoc,
				},
			}
		}),
		fx.Populate(&s.Allocations, &s.Pieces),
	}
	s.BaseSuite.SetupSuite()
}
