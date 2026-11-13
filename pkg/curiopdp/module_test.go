package curiopdp

import (
	"testing"

	"github.com/fil-forge/ucantone/multikey"
	"github.com/fil-forge/ucantone/multikey/ed25519"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx/fxtest"

	"github.com/fil-forge/piri/pkg/internal/testutil"
)

func TestProvideEngine_RegistersTheMachineUnderTheNodeDID(t *testing.T) {
	db := testutil.NewHarmonyDB(t)
	key, err := ed25519.Generate()
	require.NoError(t, err)
	id := multikey.KeyIssuer(key)

	lc := fxtest.NewLifecycle(t)
	eng, err := provideEngine(lc, db, id, nil)
	require.NoError(t, err)
	t.Cleanup(lc.RequireStop)

	require.Equal(t, id.DID().String(), eng.Host())

	var machines []struct {
		HostAndPort string `db:"host_and_port"`
	}
	require.NoError(t, db.Select(t.Context(), &machines, `SELECT host_and_port FROM harmony_machines`))
	require.Len(t, machines, 1)
	require.Equal(t, id.DID().String(), machines[0].HostAndPort)
}
