package ucanfx

import (
	"fmt"
	"time"

	"go.uber.org/fx"

	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/did/key"
	"github.com/fil-forge/ucantone/did/plc"
	"github.com/fil-forge/ucantone/did/resolver"
	"github.com/fil-forge/ucantone/did/web"

	// Registers the secp256k1 verification method used by did:plc DID documents.
	_ "github.com/fil-forge/ucantone/multikey/secp256k1/verifier"
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

		newDIDResolver,

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
		ucanhandlers.ProvideRPCOption(func(resolver did.Resolver) server.HTTPOption {
			return server.WithValidationOptions(
				validator.WithDIDResolver(resolver),
			)
		}),
		ucanhandlers.ProvideRetrievalOption(func(resolver did.Resolver) server.HTTPOption {
			return server.WithValidationOptions(
				validator.WithDIDResolver(resolver),
			)
		}),
	),

	access.Module,
	blob.Module,
	//replica.Module, // re-enable: see #15
	content.Module,
	pdp.Module,
)

// newDIDResolver builds the DID resolver used to validate incoming UCANs. It
// always supports did:key and did:web, and additionally supports did:plc when a
// PLC directory URL is configured.
func newDIDResolver(cfg app.UCANServiceConfig) (did.Resolver, error) {
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
	p, err := plc.NewResolver(cfg.PLCDirectory)
	if err != nil {
		return nil, fmt.Errorf("could not create did:plc resolver: %w", err)
	}
	m := resolver.ByMethod{
		"key": key.Resolver,
		"web": resolver.NewCached(httpResolver, 24*time.Hour),
		"plc": resolver.NewCached(p, 3*time.Hour),
	}
	return m, nil
}
