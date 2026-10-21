package status

import (
	"os"

	"github.com/fil-forge/piri/pkg/client"
	"github.com/spf13/cobra"
)

var upgradeCheckCmd = &cobra.Command{
	Use:   "upgrade-check",
	Short: "Check if it's safe to upgrade",
	Long: `Check if the node is in a state where it's safe to perform an upgrade.

Exit codes:
  0 - Safe to upgrade
  1 - Not safe to upgrade
  2 - Unable to determine status

This command is designed for use in scripts and automation.`,
	RunE: runUpgradeCheck,
}

// Test seams: the node status source and process exit.
var (
	getNodeStatus = client.GetNodeStatus
	exit          = os.Exit
)

const (
	exitNotSafe           = 1
	exitUnableToDetermine = 2
)

func init() {
	upgradeCheckCmd.SetOut(os.Stdout)
	upgradeCheckCmd.SetErr(os.Stderr)
}

func runUpgradeCheck(cmd *cobra.Command, _ []string) error {
	// Flags and args are valid by now; failures from here on are runtime
	// conditions, so don't print usage for them.
	cmd.SilenceUsage = true

	ctx := cmd.Context()

	status, err := getNodeStatus(ctx)
	if err != nil {
		cmd.PrintErrln("Unable to determine node status:", err)
		exit(exitUnableToDetermine)
		return nil
	}

	if !status.UpgradeSafe {
		// Exit directly rather than returning an error, so the reason is the
		// only thing printed: a returned error would be echoed again by cobra
		// and by the root command's log.Fatal.
		cmd.PrintErrln("Not safe to upgrade:", status.UnsafeReason)
		exit(exitNotSafe)
		return nil
	}

	cmd.Println("Safe to upgrade")
	return nil
}
