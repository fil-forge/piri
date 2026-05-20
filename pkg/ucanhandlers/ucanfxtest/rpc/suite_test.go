package rpc_test

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

	UploadServiceIdentity principal.Signer
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
		// Swap the production HTTP/cached resolver for local resolution.
		// did:key DIDs encode their public key directly so we decode in
		// process — no network, works for any test signer (Alice, Bob,
		// Mallory, etc.). did:web still needs the map for WebService.
		fx.Decorate(func(validator.VerifierResolverMap) validator.VerifierResolverMap {
			return validator.VerifierResolverMap{
				"key": resolveDIDKey,
				"web": wrapWebResolver(webResolver),
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

// resolveDIDKey decodes a did:key DID into a Verifier in process. The DID
// itself encodes the public key bytes, so no network or static map is
// needed — works for any test signer the suite mints.
func resolveDIDKey(_ context.Context, d did.DID) (ucan.Verifier, error) {
	return verifier.FromDIDKey(d)
}

// wrapWebResolver wraps a did:web resolver so the returned verifier's
// DID() matches the input did:web (not the underlying did:key).
//
// TODO(file an issue): libforge's didresolver.NewMapResolver stores the
// unwrapped did:key verifier in its map, so MapResolver.Resolve returns
// a verifier whose DID() is the did:key, not the did:web that was
// looked up. ucantone's token.VerifySignature (token/token.go:12) then
// rejects with an issuer/verifier DID mismatch BEFORE the signature is
// even checked, producing a confusing "InvalidSignature" failure that
// reads like a signing key problem. Until libforge wraps in
// NewMapResolver (or until something else exposes a did:web-preserving
// helper), the suite wraps in test code so tok.Issuer() (did:web) ==
// verifier.DID() (did:web) and the signature check actually runs.
//
// Repro details:
//   - libforge/didresolver/mapresolver.go:36 → verifier.Parse(v) drops the requested DID
//   - ucantone/ucan/token/token.go:12 → tok.Issuer() != verifier.DID() rejects without verifying
func wrapWebResolver(r *didresolver.MapResolver) validator.DIDVerifierResolverFunc {
	return func(ctx context.Context, d did.DID) (ucan.Verifier, error) {
		plain, err := r.Resolve(ctx, d)
		if err != nil {
			return nil, err
		}
		return &didWrappedVerifier{did: d, inner: plain}, nil
	}
}

// didWrappedVerifier overrides the wrapped verifier's DID() with the
// resolver's input DID, preserving the underlying signature check.
type didWrappedVerifier struct {
	did   did.DID
	inner ucan.Verifier
}

func (w *didWrappedVerifier) DID() did.DID                { return w.did }
func (w *didWrappedVerifier) Verify(msg, sig []byte) bool { return w.inner.Verify(msg, sig) }
