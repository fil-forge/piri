package app_test

import (
	"testing"

	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/config"
	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/config/dynamic"
	fxapp "github.com/fil-forge/piri/pkg/fx/app"
	"github.com/fil-forge/piri/pkg/internal/testutil"
	"github.com/fil-forge/piri/pkg/pdp/piecesize"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCommonModules_PieceSizePolicy pins two things that are easy to break
// silently.
//
// First, that the piece-size limit is exposed on the admin config surface at
// all. fx constructors are lazy and the constructor is what registers the key,
// so without the fx.Invoke in CommonModules the key would vanish from
// GET /admin/config the moment the last consumer stopped depending on it.
//
// Second, that a zero-valued PDPService config still yields a working policy.
// testutil.NewTestConfig does not populate PDPService, so every fx-backed test
// suite depends on that.
func TestCommonModules_PieceSizePolicy(t *testing.T) {
	cfg := testutil.NewTestConfig(t, func(_ *testing.T, c *app.AppConfig) {
		c.Storage.DataDir = ""
		c.Storage.S3 = nil
	})
	require.Zero(t, cfg.PDPService.Piece.MaxPaddedSize,
		"precondition: the test config leaves PDPService zero-valued")

	var (
		registry *dynamic.Registry
		policy   piecesize.Policy
	)
	fxtestApp := fx.New(
		fx.NopLogger,
		fxapp.CommonModules(cfg),
		fx.Populate(&registry, &policy),
	)
	require.NoError(t, fxtestApp.Err())

	assert.Contains(t, registry.Keys(), config.PieceMaxPaddedSize,
		"the piece size limit must be visible to the admin config API")

	assert.Equal(t, piecesize.DefaultMaxPaddedSize, policy.Limits().MaxPadded())
	assert.Equal(t, uint64(266338304), policy.MaxRaw())
}
