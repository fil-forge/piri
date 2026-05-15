package principalresolver

import (
	"fmt"
	"strings"
	"time"

	"go.uber.org/fx"

	"github.com/fil-forge/ucantone/did"
	ucanserver "github.com/fil-forge/ucantone/server"
	"github.com/fil-forge/ucantone/validator"

	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/principalresolver"
)

var Module = fx.Module("principalresolver",
	fx.Provide(
		NewPrincipalResolver,
		// The same option feeds both the storage- and retrieval-side UCAN
		// server groups. fx.Annotate allows only one ResultTags, so provide
		// it once per group (ProvideAsUCANOption is pure, so this is cheap).
		fx.Annotate(
			ProvideAsUCANOption,
			fx.ResultTags(`group:"ucan_options"`),
		),
		fx.Annotate(
			ProvideAsUCANOption,
			fx.ResultTags(`group:"retrieval_ucan_options"`),
		),
	),
)

// didFromConn extracts the configured service DID from a ServiceConnection
// when the DID method is `did:web`. Returns ok=false for nil connections,
// non-`did:web` services, or unknown connection shapes.
func didFromConn(conn any) (did.DID, bool) {
	c, ok := conn.(*app.ServiceConnection)
	if !ok || c == nil {
		return did.DID{}, false
	}
	if !strings.HasPrefix(c.DID.String(), "did:web:") {
		return did.DID{}, false
	}
	return c.DID, true
}

// NewPrincipalResolver creates a principal resolver from configuration.
//
// Only `did:web` upstream services are registered for HTTP-based DID
// resolution; `did:key` services authenticate via their key material
// without needing a resolver entry.
func NewPrincipalResolver(cfg app.AppConfig) (*principalresolver.CachedResolver, error) {
	services := make([]did.DID, 0, 2)
	if d, ok := didFromConn(cfg.UCANService.Services.Indexer.Connection); ok {
		services = append(services, d)
	}
	if d, ok := didFromConn(cfg.UCANService.Services.Upload.Connection); ok {
		services = append(services, d)
	}

	var opts []principalresolver.Option
	if cfg.UCANService.InsecureDIDResolution {
		opts = append(opts, principalresolver.InsecureResolution())
	}

	hr, err := principalresolver.NewHTTPResolver(services, opts...)
	if err != nil {
		return nil, fmt.Errorf("creating http principal resolver: %w", err)
	}
	cr, err := principalresolver.NewCachedResolver(hr, 24*time.Hour)
	if err != nil {
		return nil, fmt.Errorf("creating cached principal resolver: %w", err)
	}
	return cr, nil
}

// ProvideAsUCANOption provides the principal resolver as a storage-side
// UCAN HTTP server option (via the validator's DIDResolver hook).
func ProvideAsUCANOption(resolver *principalresolver.CachedResolver) ucanserver.HTTPOption {
	return ucanserver.WithValidationOptions(validator.WithDIDResolver(resolver.Resolve))
}
