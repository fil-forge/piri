package ucanfx

import (
	"fmt"
	"runtime/debug"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"go.uber.org/fx"

	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/did/key"
	"github.com/fil-forge/ucantone/did/plc"
	"github.com/fil-forge/ucantone/did/resolver"
	"github.com/fil-forge/ucantone/did/web"
	"github.com/fil-forge/ucantone/execution"

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

var log = logging.Logger("ucan")

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

		// The dispatcher recovers panics raised while executing an
		// invocation and answers with an ExecutionFailure receipt. Route
		// the recovered value to piri's logger instead of the standard
		// log package.
		ucanhandlers.ProvideRPCOption(func() server.HTTPOption {
			return server.WithPanicLogger(logPanic)
		}),
		ucanhandlers.ProvideRetrievalOption(func() server.HTTPOption {
			return server.WithPanicLogger(logPanic)
		}),
	),

	access.Module,
	blob.Module,
	//replica.Module, // re-enable: see #15
	content.Module,
	pdp.Module,
)

// logPanic records a panic recovered by the UCAN dispatcher. It runs on the
// panicking goroutine inside the deferred recover, so debug.Stack returns the
// stack of the panic.
func logPanic(req execution.Request, value any) {
	log.Errorw("panic executing UCAN invocation",
		"command", req.Invocation().Command(),
		"task", req.Invocation().Task().Link(),
		"panic", value,
		"stack", string(debug.Stack()),
	)
}

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
