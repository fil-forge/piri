package publisher

import (
	"context"

	"github.com/fil-forge/ucantone/ucan"
)

type Publisher interface {
	// Publish advertises content claims/commitments found on this node to the
	// storacha network.
	Publish(context.Context, ucan.Invocation) error
}
