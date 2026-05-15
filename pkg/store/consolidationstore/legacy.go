package consolidationstore

import (
	"context"

	"github.com/ipfs/go-cid"

	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/consolidationstore/consolidation"
)

// LegacyReader reads consolidations from a deprecated on-disk format. The
// UCAN 0.x → 1.0 migration removed the only legacy implementation
// (DatastoreLegacyReader), which decoded track invocations from a
// CAR-archived legacy delegation format. NoOpLegacyReader is now the only
// implementation.
type LegacyReader interface {
	// Get retrieves a consolidation from the legacy format.
	// Returns store.ErrNotFound if the consolidation does not exist.
	Get(ctx context.Context, batchCID cid.Cid) (consolidation.Consolidation, error)
	// Delete removes a consolidation from the legacy format.
	Delete(ctx context.Context, batchCID cid.Cid) error
}

// NoOpLegacyReader is a LegacyReader that always returns ErrNotFound. The
// UCAN 1.0 migration changed the on-disk encoding of track invocations
// incompatibly, so prior data cannot be lazily migrated; existing deployments
// will lose previously persisted consolidation state.
type NoOpLegacyReader struct{}

var _ LegacyReader = (*NoOpLegacyReader)(nil)

func (NoOpLegacyReader) Get(ctx context.Context, batchCID cid.Cid) (consolidation.Consolidation, error) {
	return consolidation.Consolidation{}, store.ErrNotFound
}

func (NoOpLegacyReader) Delete(ctx context.Context, batchCID cid.Cid) error {
	return nil
}

// NewDatastoreLegacyReader returns a no-op legacy reader. The previous
// implementation that read CAR-archived UCAN 0.x track invocations from
// "track/" and "consolidate/" datastore namespaces has been removed.
func NewDatastoreLegacyReader(_ any) LegacyReader {
	return NoOpLegacyReader{}
}
