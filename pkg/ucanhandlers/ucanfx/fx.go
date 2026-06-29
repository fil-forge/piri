package ucanfx

import (
	"fmt"
	"time"

	"go.uber.org/fx"

	"github.com/fil-forge/libforge/attestation"
	"github.com/fil-forge/libforge/attestation/didmailto"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/did/key"
	"github.com/fil-forge/ucantone/did/resolver"
	"github.com/fil-forge/ucantone/did/web"
	"github.com/fil-forge/ucantone/server"
	"github.com/fil-forge/ucantone/validator"

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

		func(cfg app.UCANServiceConfig) (did.Resolver, error) {
			var (
				httpResolver did.Resolver
				err          error
			)
			if cfg.InsecureDIDResolution {
				httpResolver, err = web.NewResolver(web.WithInsecure(true))
			} else {
				httpResolver, err = web.NewResolver()
			}
			if err != nil {
				return nil, fmt.Errorf("could not create http resolver: %w", err)
			}

			// did:mailto identities (the account DIDs at the root of
			// most proof chains) have no native key material — they are
			// signed by attestation from a trusted authority, the upload
			// service. The resolver synthesizes a DID document whose
			// AuthorityAttestation verification method names that
			// authority; the matching verifier factory (below) turns it
			// into an AttestedVerifier. Without the mailto entry,
			// resolving an account DID fails with "unsupported DID
			// method: mailto" and every invocation whose proof chain
			// traces back to a did:mailto account is rejected.
			return resolver.ByMethod{
				"key":    key.Resolver,
				"web":    resolver.NewCached(httpResolver, 24*time.Hour),
				"mailto": didmailto.NewResolver(cfg.Services.Upload.DID),
			}, nil
		},

		// Verifier factories for the validator. The defaults cover
		// Multikey (did:key / did:web) verification methods; we add the
		// AuthorityAttestation factory so the validator can verify
		// attested signatures from did:mailto principals. The factory
		// resolves the authority's own DID document (e.g. did:web:upload)
		// via the same resolver, so it must see the full resolver.
		func(res did.Resolver) map[string]validator.VerifierFactory {
			factories := validator.DefaultFactories()
			factories[attestation.Type] = attestation.NewVerifierFactory(res, factories)
			return factories
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
		ucanhandlers.ProvideRPCOption(func(resolver did.Resolver, factories map[string]validator.VerifierFactory) server.HTTPOption {
			return server.WithValidationOptions(
				validator.WithDIDResolver(resolver),
				validator.WithVerifierFactories(factories),
			)
		}),
		ucanhandlers.ProvideRetrievalOption(func(resolver did.Resolver, factories map[string]validator.VerifierFactory) server.HTTPOption {
			return server.WithValidationOptions(
				validator.WithDIDResolver(resolver),
				validator.WithVerifierFactories(factories),
			)
		}),
	),

	access.Module,
	blob.Module,
	//replica.Module, // re-enable: see #15
	content.Module,
	pdp.Module,
)
