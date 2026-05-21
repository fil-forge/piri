package ucanfx

import (
	"fmt"
	"time"

	"github.com/fil-forge/libforge/didresolver"
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
				if err != nil {
					return nil, fmt.Errorf("could not create http resolver: %w", err)
				}
			} else {
				httpResolver, err = didresolver.NewHTTPResolver()
				if err != nil {
					return nil, fmt.Errorf("could not create http resolver: %w", err)
				}
			}

			cachedRes, err := didresolver.NewCachedResolver(httpResolver.Resolve, 24*time.Hour)
			if err != nil {
				return nil, fmt.Errorf("could not create cached resolver: %w", err)
			}

			// did:key is self-describing — resolve it locally rather than
			// over HTTP, which only makes sense for did:web.
			return validator.VerifierResolverMap{
				"key": validator.ResolveDIDKeyVerifier,
				"web": cachedRes.Resolve,
			}, nil
		},

		// Server-wide options. Stamp every receipt with a server-side
		// issuance timestamp on both transports.
		ucanhandlers.ProvideRPCOption(func(resolver validator.VerifierResolverMap) server.HTTPOption {
			return server.WithValidationOptions(validator.WithDIDVerifierResolvers(resolver))
		}),
		// An example of how options may be provided to the retrieval server
		/*
			ucanhandlers.ProvideRetrievalOption(func() server.HTTPOption {
				return server.WithReceiptTimestamps(true)
			}),
		*/
	),

	access.Module,
	blob.Module,
	//replica.Module, // re-enable: see #15
	content.Module,
	pdp.Module,
)
