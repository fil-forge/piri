package publisher

import (
	"context"

	"github.com/fil-forge/go-ipni-tools/pkg/store"
	"github.com/fil-forge/ucantone/ucan/invocation"
)

type Publisher interface {
	// Store is the storage interface for published advertisements.
	Store() store.PublisherStore
	// Publish advertises a signed content claim invocation to the storacha
	// network (currently: an IPNI advertisement).
	Publish(context.Context, *invocation.Invocation) error
}
