package curiopdp

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/fx/fxtest"

	"github.com/fil-forge/piri/pkg/internal/testutil"
)

func TestProvideEngine_RegistersTheMachineUnderTheGivenID(t *testing.T) {
	db := testutil.NewHarmonyDB(t)
	const id = "did:key:z6MkhaXgBZDvotDkL5257faiztiGiC2QtKLGpbnnEGta2doK"

	lc := fxtest.NewLifecycle(t)
	eng, err := provideEngine(lc, db, id, nil)
	require.NoError(t, err)
	lc.RequireStart()
	t.Cleanup(lc.RequireStop)

	require.Equal(t, id, eng.Host())

	var machines []struct {
		HostAndPort string `db:"host_and_port"`
	}
	require.NoError(t, db.Select(t.Context(), &machines, `SELECT host_and_port FROM harmony_machines`))
	require.Len(t, machines, 1)
	require.Equal(t, id, machines[0].HostAndPort)
}
