package status

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/client"
)

// runUpgradeCheckWith runs the upgrade-check command against a stubbed node
// status and returns stdout, stderr and the exit code requested (0 if exit was
// never called).
func runUpgradeCheckWith(t *testing.T, status *client.NodeStatus, statusErr error) (string, string, int) {
	t.Helper()

	origGet, origExit := getNodeStatus, exit
	t.Cleanup(func() { getNodeStatus, exit = origGet, origExit })

	getNodeStatus = func(context.Context) (*client.NodeStatus, error) { return status, statusErr }
	code := 0
	exit = func(c int) { code = c }

	var stdout, stderr bytes.Buffer
	cmd := &cobra.Command{Use: "upgrade-check", RunE: runUpgradeCheck}
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{})

	require.NoError(t, cmd.ExecuteContext(context.Background()))
	require.True(t, cmd.SilenceUsage, "runtime failures must not print usage")
	return stdout.String(), stderr.String(), code
}

func TestUpgradeCheck(t *testing.T) {
	const reason = "proof set 7 is in a fault state: challenge window opened at epoch 1000 (next challenge epoch) and closed at epoch 1060 without a proof; current epoch is 1100"

	t.Run("not safe prints the reason once and exits 1", func(t *testing.T) {
		stdout, stderr, code := runUpgradeCheckWith(t, &client.NodeStatus{
			Healthy:      true,
			InFaultState: true,
			UpgradeSafe:  false,
			UnsafeReason: reason,
		}, nil)

		require.Equal(t, exitNotSafe, code)
		require.Empty(t, stdout)
		require.Equal(t, "Not safe to upgrade: "+reason+"\n", stderr)
		require.Equal(t, 1, strings.Count(stderr, reason))
		require.Equal(t, 1, strings.Count(strings.ToLower(stderr), "not safe"))
		require.NotContains(t, stderr, "Usage:")
	})

	t.Run("safe prints confirmation and exits 0", func(t *testing.T) {
		stdout, stderr, code := runUpgradeCheckWith(t, &client.NodeStatus{Healthy: true, UpgradeSafe: true}, nil)

		require.Equal(t, 0, code)
		require.Equal(t, "Safe to upgrade\n", stdout)
		require.Empty(t, stderr)
	})

	t.Run("status error exits 2", func(t *testing.T) {
		_, stderr, code := runUpgradeCheckWith(t, nil, errors.New("connection refused"))

		require.Equal(t, exitUnableToDetermine, code)
		require.Equal(t, "Unable to determine node status: connection refused\n", stderr)
		require.NotContains(t, stderr, "Usage:")
	})
}
