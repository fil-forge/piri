package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/fil-forge/piri/pkg/build"
	"github.com/fil-forge/piri/pkg/config"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version of piri",
	Long: `Print the version of piri including the git revision.

With --config-version, print only the version of the config ` + "`piri init`" + ` generates,
for scripts that re-run init when it changes.`,
	Run: func(cmd *cobra.Command, args []string) {
		out := cmd.OutOrStdout()
		if configVersion, _ := cmd.Flags().GetBool("config-version"); configVersion {
			fmt.Fprintln(out, config.GeneratedConfigVersion)
			return
		}
		fmt.Fprintf(out, "version: %s\n", build.Version)
		fmt.Fprintf(out, "commit: %s\n", build.Commit)
		fmt.Fprintf(out, "built at: %s\n", build.Date)
		fmt.Fprintf(out, "built by: %s\n", build.BuiltBy)
	},
}

func init() {
	versionCmd.Flags().Bool("config-version", false, "print only the version of the config piri init generates")
	rootCmd.AddCommand(versionCmd)
}
