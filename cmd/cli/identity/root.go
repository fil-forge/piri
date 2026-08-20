package identity

import (
	"fmt"
	"io"
	"os"

	"github.com/fil-forge/libforge/identity"
	"github.com/fil-forge/ucantone/multikey/ed25519"
	"github.com/spf13/cobra"
)

var (
	Cmd = &cobra.Command{
		Use:     "identity",
		Aliases: []string{"id"},
		Short:   "identity commands",
		Long: `This command provides a set of subcommands for working with decentralized identities.
Specifically for generating and managing Ed25519 keys used in DID (Decentralized Identifier) systems.
  - Generate new Ed25519 key pairs encoded in PEM-format
  - Extract DID from PEM file
`,
	}

	GenerateCmd = &cobra.Command{
		Use:     "generate",
		Aliases: []string{"gen"},
		Args:    cobra.NoArgs,
		Short:   "generate an identity",
		Long: `Generate a new PEM-encoded Ed25519 key pair for use with decentralized identities (DIDs).
The command will output the key to stdout, which can be redirected to a file. 
The DID is printed to stderr for convenience.
`,
		Example: "piri identity generate > my-key.pem",
		RunE:    doGenerate,
	}

	ParseCmd = &cobra.Command{
		Use:     "parse",
		Short:   "parse a DID from a PEM file containing an Ed25519 key",
		Args:    cobra.ExactArgs(1),
		Example: `piri identity parse my-key.pem`,
		RunE:    doParse,
	}
)

func init() {
	Cmd.AddCommand(GenerateCmd)
	Cmd.AddCommand(ParseCmd)
	Cmd.SetHelpFunc(identityHelpFunc())
	GenerateCmd.SetHelpFunc(identityHelpFunc())
	ParseCmd.SetHelpFunc(identityHelpFunc())
}

func doGenerate(cmd *cobra.Command, _ []string) error {
	signer, err := ed25519.GenerateIssuer()
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}
	pem, err := identity.EncodeSignerToPEM(signer)
	if err != nil {
		return fmt.Errorf("encoding ed25519 private key to PEM: %w", err)
	}
	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)
	cmd.PrintErrf("# %s\n", signer.DID())
	cmd.Print(string(pem))
	return nil
}

func doParse(cmd *cobra.Command, args []string) error {
	pemPath := args[0]
	pemFile, err := os.Open(pemPath)
	if err != nil {
		return fmt.Errorf("opening pem file: %w", err)
	}
	pemData, err := io.ReadAll(pemFile)
	if err != nil {
		return fmt.Errorf("reading pem file: %w", err)
	}
	key, err := identity.DecodeSignerFromPEM(pemData)
	if err != nil {
		return fmt.Errorf("decoding ed25519 private key: %w", err)
	}
	cmd.Printf("# %s\n", key.KeyDID().String())
	return nil
}

func identityHelpFunc() func(cmd *cobra.Command, args []string) {
	return func(cmd *cobra.Command, args []string) {
		fmt.Printf("Usage:\n  %s\n\n", cmd.UseLine())
		fmt.Printf("%s\n", cmd.Short)
		if cmd.Long != "" {
			fmt.Printf("\n%s\n", cmd.Long)
		}

		// Show available subcommands
		if cmd.HasAvailableSubCommands() {
			fmt.Printf("\nAvailable Commands:\n")
			for _, c := range cmd.Commands() {
				if c.IsAvailableCommand() {
					fmt.Printf("  %-15s %s\n", c.Name(), c.Short)
				}
			}
		}
		// Only show local flags
		if cmd.HasLocalFlags() {
			fmt.Printf("\nFlags:\n")
			fmt.Print(cmd.LocalFlags().FlagUsages())
		}
	}
}
