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

With --config, print only the version of the config ` + "`piri init`" + ` generates,
for scripts that re-run init when it changes.`,
	Run: func(cmd *cobra.Command, args []string) {
		if configOnly, _ := cmd.Flags().GetBool("config"); configOnly {
			fmt.Println(config.GeneratedConfigVersion)
			return
		}
		fmt.Printf("version: %s\n", build.Version)
		fmt.Printf("commit: %s\n", build.Commit)
		fmt.Printf("built at: %s\n", build.Date)
		fmt.Printf("built by: %s\n", build.BuiltBy)
	},
}

func init() {
	versionCmd.Flags().Bool("config", false, "print only the version of the config `piri init` generates")
	rootCmd.AddCommand(versionCmd)
}
