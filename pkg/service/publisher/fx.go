package publisher

import (
	"fmt"

	"github.com/fil-forge/go-libstoracha/ipnipublisher/store"
	"github.com/fil-forge/go-ucanto/principal"
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/config/app"
	echofx "github.com/fil-forge/piri/pkg/fx/echo"
)

var Module = fx.Module("publisher",
	fx.Provide(
		// Also provide the interface
		fx.Annotate(
			NewFx,
			fx.As(new(Publisher)),
		),
		fx.Annotate(
			NewServer,
			fx.As(new(echofx.RouteRegistrar)),
			fx.ResultTags(`group:"route_registrar"`),
		),
	),
)

func NewFx(
	cfg app.AppConfig,
	id principal.Signer,
	publisherStore store.PublisherStore,
) (*PublisherService, error) {
	pubCfg := cfg.UCANService.Services.Publisher
	if pubCfg.PublicMaddr.String() == "" {
		return nil, fmt.Errorf("public address is required for publisher service")
	}

	return New(
		id,
		publisherStore,
		pubCfg.PublicMaddr,
		WithDirectAnnounce(pubCfg.AnnounceURLs...),
		WithIndexingService(cfg.UCANService.Services.Indexer.Connection),
		WithIndexingServiceProof(cfg.UCANService.Services.Indexer.Proofs...),
		WithAnnounceAddress(pubCfg.AnnounceMaddr),
		WithBlobAddress(pubCfg.BlobMaddr),
	)
}
