package publisher

import (
	"fmt"

	"github.com/fil-forge/go-ipni-tools/pkg/store"
	"github.com/fil-forge/libforge/identity"
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/config/app"
	echofx "github.com/fil-forge/piri/pkg/fx/echo"
)

var Module = fx.Module("publisher",
	fx.Provide(
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

// NewFx wires the publisher service from its narrow configs.
func NewFx(
	pubCfg app.PublisherServiceConfig,
	idxCfg app.IndexingServiceConfig,
	id identity.Identity,
	publisherStore store.PublisherStore,
) (*PublisherService, error) {
	if pubCfg.PublicMaddr.String() == "" {
		return nil, fmt.Errorf("public address is required for publisher service")
	}

	return New(
		id,
		publisherStore,
		pubCfg.PublicMaddr,
		WithDirectAnnounce(pubCfg.AnnounceURLs...),
		WithIndexingService(idxCfg),
		WithIndexingServiceProof(idxCfg.Proofs),
		WithAnnounceAddress(pubCfg.AnnounceMaddr),
		WithBlobAddress(pubCfg.BlobMaddr),
	)
}
