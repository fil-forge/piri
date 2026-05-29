package retrieval_test

import (
	"context"
	"net/url"
	"testing"

	"github.com/fil-forge/libforge/didresolver"
	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/principal"
	"github.com/fil-forge/ucantone/principal/verifier"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/validator"
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

	UploadServiceIdentity principal.Signer
	UploadServiceURL      *url.URL
}

func TestRetrievalSuite(t *testing.T) {
	suite.Run(t, new(RetrievalSuite))
}

func (s *RetrievalSuite) SetupSuite() {
	s.ServiceID = testutil.Alice
	s.UploadServiceIdentity = testutil.WebService
	s.UploadServiceURL = testutil.TestURL

	s.ConfigOptions = []piritestutil.TestConfigOption{
		piritestutil.WithUploadServiceConfig(s.UploadServiceIdentity.DID(), s.UploadServiceURL),
	}

	// Map resolver handles did:web → did:key indirection for the upload
	// service identity (testutil.WebService wraps testutil.Service).
	webResolver, err := didresolver.NewMapResolver(map[string]string{
		s.UploadServiceIdentity.DID().String(): testutil.Service.DID().String(),
	})
	s.Require().NoError(err)

	s.ExtraOptions = []fx.Option{
		// did:key resolves in-process (the DID itself encodes the public
		// key); did:web goes through the static map for WebService.
		fx.Decorate(func(validator.VerifierResolverMap) validator.VerifierResolverMap {
			return validator.VerifierResolverMap{
				"key": resolveDIDKey,
				"web": webResolver.Resolve,
			}
		}),
		fx.Populate(&s.Allocations, &s.Pieces),
	}
	s.BaseSuite.SetupSuite()
}

// resolveDIDKey decodes a did:key DID into a Verifier in process. The DID
// itself encodes the public key bytes, so no network or static map is
// needed — works for any test signer the suite mints.
func resolveDIDKey(_ context.Context, d did.DID) (ucan.Verifier, error) {
	return verifier.FromDIDKey(d)
}
