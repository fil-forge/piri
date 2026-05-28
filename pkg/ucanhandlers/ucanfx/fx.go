package ucanfx

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/fil-forge/libforge/commands/ucan/attest"
	"github.com/fil-forge/libforge/didresolver"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/principal/verifier"
	"github.com/fil-forge/ucantone/ucan"
	ucantoken "github.com/fil-forge/ucantone/ucan/token"
	"github.com/fil-forge/ucantone/validator"
	"go.uber.org/fx"

	"github.com/fil-forge/ucantone/server"

	"github.com/fil-forge/piri/pkg/config/app"
	echofx "github.com/fil-forge/piri/pkg/fx/echo"
	"github.com/fil-forge/piri/pkg/ucanhandlers"
	"github.com/fil-forge/piri/pkg/ucanhandlers/access"
	"github.com/fil-forge/piri/pkg/ucanhandlers/blob"
	"github.com/fil-forge/piri/pkg/ucanhandlers/content"
	"github.com/fil-forge/piri/pkg/ucanhandlers/pdp"
)

// Module composes the UCAN HTTP surface. It builds the two servers
// (body-CAR RPC + header-container retrieval), exposes each as an echo
// RouteRegistrar, and pulls in the per-capability sub-modules that
// register their handlers via the ucanhandlers group tags.
var Module = fx.Module("ucan",
	fx.Provide(
		fx.Annotate(
			ucanhandlers.NewRPC,
			fx.As(new(echofx.RouteRegistrar)),
			fx.ResultTags(`group:"route_registrar"`),
		),
		fx.Annotate(
			ucanhandlers.NewRetrieval,
			fx.As(new(echofx.RouteRegistrar)),
			fx.ResultTags(`group:"route_registrar"`),
		),

		func(cfg app.UCANServiceConfig) (validator.VerifierResolverMap, error) {
			var (
				httpResolver *didresolver.HTTPResolver
				err          error
			)
			if cfg.InsecureDIDResolution {
				httpResolver, err = didresolver.NewHTTPResolver(didresolver.InsecureResolution())
			} else {
				httpResolver, err = didresolver.NewHTTPResolver()
			}
			if err != nil {
				return nil, fmt.Errorf("could not create http resolver: %w", err)
			}

			cachedRes, err := didresolver.NewCachedResolver(httpResolver.Resolve, 24*time.Hour)
			if err != nil {
				return nil, fmt.Errorf("could not create cached resolver: %w", err)
			}

			// did:key is self-describing — resolve it locally rather than
			// over HTTP, which only makes sense for did:web.
			resolveDIDKey := func(ctx context.Context, did did.DID) (ucan.Verifier, error) {
				return verifier.FromDIDKey(did)
			}
			return validator.VerifierResolverMap{
				"key": resolveDIDKey,
				"web": cachedRes.Resolve,
			}, nil
		},

		// Trust attestations issued by the Forge upload service
		func(cfg app.UCANServiceConfig, resolvers validator.VerifierResolverMap) validator.NonStandardSignatureVerifierFunc {
			return newAttestationVerifier(cfg.Services.Upload.DID, resolvers)
		},

		// Server-wide options. Both transports need the DID verifier
		// resolvers so they can validate UCANs signed by did:web identities
		// (e.g. did:web:indexer, did:web:upload). Without the retrieval
		// server option below, the retrieval dispatcher rejects every
		// invocation from a did:web issuer with "unsupported DID method:
		// web". The resulting failure receipt has no HTTP metadata, so
		// retrieval/server.go's RoundTrip falls through to the codec
		// default — 200 OK with empty body and the failure receipt
		// hidden in the X-UCAN-Container header — which downstream
		// clients (the indexer's blobindexlookup) mis-read as
		// success-with-empty-body and then choke on CAR decode EOF.
		ucanhandlers.ProvideRPCOption(func(resolver validator.VerifierResolverMap, verifyNonStandardSig validator.NonStandardSignatureVerifierFunc) server.HTTPOption {
			return server.WithValidationOptions(
				validator.WithDIDVerifierResolvers(resolver),
				validator.WithNonStandardSignatureVerifier(verifyNonStandardSig),
			)
		}),
		ucanhandlers.ProvideRetrievalOption(func(resolver validator.VerifierResolverMap, verifyNonStandardSig validator.NonStandardSignatureVerifierFunc) server.HTTPOption {
			return server.WithValidationOptions(
				validator.WithDIDVerifierResolvers(resolver),
				validator.WithNonStandardSignatureVerifier(verifyNonStandardSig),
			)
		}),
	),

	access.Module,
	blob.Module,
	//replica.Module, // re-enable: see #15
	content.Module,
	pdp.Module,
)

// newAttestationVerifier creates a [validator.NonStandardSignatureVerifierFunc]
// that validates that a delegation is attested by the given authority.
func newAttestationVerifier(authority did.DID, resolvers validator.VerifierResolverMap) validator.NonStandardSignatureVerifierFunc {
	return func(ctx context.Context, token ucan.Token, meta ucan.Container) error {
		resolver, ok := resolvers[authority.Method()]
		if !ok {
			return fmt.Errorf("no resolver for DID method: %s", authority.Method())
		}
		verifier, err := resolver(ctx, authority)
		if err != nil {
			return fmt.Errorf("could not resolve DID: %w", err)
		}
		// We only support attestations as delegations - attested delegation MUST
		// delegate to an agent DID which is then used in the invocation.
		dlg, ok := token.(ucan.Delegation)
		if !ok {
			return fmt.Errorf("token is not a delegation")
		}
		for _, inv := range meta.Invocations() {
			if inv.Command() != attest.Proof.Command {
				continue
			}
			// only trust attestations authority issued
			if inv.Issuer() != authority || inv.Subject() == did.Undef || inv.Subject() != authority {
				continue
			}
			var args attest.ProofArguments
			if err := args.UnmarshalCBOR(bytes.NewReader(inv.ArgumentsBytes())); err != nil {
				continue
			}
			// make sure the attestation is for the delegation in question
			if args.Proof != dlg.Link() {
				continue
			}
			// finally, make sure the signature is valid
			ok, err := ucantoken.VerifySignature(inv, verifier)
			if !ok || err != nil {
				continue
			}
			return nil
		}
		return fmt.Errorf("no valid attestation found for delegation")
	}
}
