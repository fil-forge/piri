// Package policy binds the piece-size limits to the dynamic config registry,
// so an operator can retune them at runtime via `piri client admin config set`
// or PATCH /admin/config without restarting the node.
//
// It is separate from pkg/pdp/piecesize because pkg/config imports piecesize
// (to validate operator input) and pkg/config/dynamic imports pkg/config;
// putting the registry glue in piecesize itself would close that loop.
package policy

import (
	"fmt"

	"github.com/fil-forge/piri/pkg/config"
	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/config/dynamic"
	"github.com/fil-forge/piri/pkg/pdp/piecesize"
)

// New registers the piece-size entries with the dynamic registry and returns a
// Policy that reads through it on every check, so a config change takes effect
// immediately.
//
// A zero cfg.MaxPaddedSize is treated as the default rather than an error.
// That matters because testutil.NewTestConfig leaves PDPService entirely
// zero-valued, so every fx-backed test suite reaches this constructor with an
// empty config and must still get a working policy.
func New(registry *dynamic.Registry, cfg app.PieceConfig) (piecesize.Policy, error) {
	fallback := cfg.MaxPaddedSize
	if fallback == 0 {
		fallback = piecesize.DefaultMaxPaddedSize
	}
	if err := piecesize.ValidatePaddedSize(fallback); err != nil {
		return piecesize.Policy{}, fmt.Errorf("invalid piece config: %w", err)
	}

	if err := registry.RegisterEntries(map[config.Key]dynamic.ConfigEntry{
		config.PieceMaxPaddedSize: {
			Value: uint(fallback),
			// The schema is what stops an operator raising the limit past
			// what Curio can prove: above CurioMaxPaddedSize the prove task
			// cannot build a memtree, and a piece that large would fault
			// rather than fail loudly at ingest.
			Schema: dynamic.PowerOfTwoSchema{
				Min: uint(piecesize.MinPaddedSize),
				Max: uint(piecesize.CurioMaxPaddedSize),
			},
		},
	}); err != nil {
		return piecesize.Policy{}, fmt.Errorf("registering piece config entries: %w", err)
	}

	return piecesize.NewPolicy(func() piecesize.Limits {
		return piecesize.Limits{
			Padded: uint64(registry.GetUint(config.PieceMaxPaddedSize, uint(fallback))),
		}
	}), nil
}
