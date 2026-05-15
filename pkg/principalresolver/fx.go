package principalresolver

import (
	"fmt"
	"strings"
	"time"

	"github.com/fil-forge/go-ucanto/did"
	ucanserver "github.com/fil-forge/go-ucanto/server"
	ucanretrievalserver "github.com/fil-forge/go-ucanto/server/retrieval"
	"github.com/fil-forge/go-ucanto/validator"
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/config/app"
)

var Module = fx.Module("principalresolver",
	fx.Provide(
		NewFx,
		fx.Annotate(
			ProvideAsUCANOption,
			fx.ResultTags(`group:"ucan_options"`),
		),
		fx.Annotate(
			ProvideAsUCANRetrievalOption,
			fx.ResultTags(`group:"ucan_retrieval_options"`),
		),
	),
)

// NewFx creates a principal resolver from configuration.
func NewFx(cfg app.UCANServiceConfig) (validator.PrincipalResolver, error) {
	services := make([]did.DID, 0, 2)
	if idxSvc := cfg.Services.Indexer.Connection; idxSvc != nil {
		if strings.HasPrefix(idxSvc.ID().DID().String(), "did:web:") {
			services = append(services, idxSvc.ID().DID())
		}
	}
	if uplSvc := cfg.Services.Upload.Connection; uplSvc != nil {
		if strings.HasPrefix(uplSvc.ID().DID().String(), "did:web:") {
			services = append(services, uplSvc.ID().DID())
		}
	}
	var opts []Option
	if cfg.InsecureDIDResolution {
		opts = append(opts, InsecureResolution())
	}

	hr, err := NewHTTPResolver(services, opts...)
	if err != nil {
		return nil, fmt.Errorf("creating http principal resolver: %w", err)
	}
	cr, err := NewCachedResolver(hr, 24*time.Hour)
	if err != nil {
		return nil, fmt.Errorf("creating cached principal resolver: %w", err)
	}
	return cr, nil
}

// ProvideAsUCANOption provides the principal resolver as a UCAN server option
func ProvideAsUCANOption(resolver validator.PrincipalResolver) ucanserver.Option {
	return ucanserver.WithPrincipalResolver(resolver.ResolveDIDKey)
}

// ProvideAsUCANRetrievalOption provides the principal resolver as a UCAN
// retrieval server option.
func ProvideAsUCANRetrievalOption(resolver validator.PrincipalResolver) ucanretrievalserver.Option {
	return ucanretrievalserver.WithPrincipalResolver(resolver.ResolveDIDKey)
}
