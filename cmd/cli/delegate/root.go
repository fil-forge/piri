package delegate

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/fil-forge/libforge/capabilities/blob"
	"github.com/fil-forge/libforge/capabilities/blob/replica"
	"github.com/fil-forge/libforge/capabilities/pdp"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/principal/signer"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/delegation"
	"github.com/spf13/cobra"

	"github.com/fil-forge/piri/lib"
	"github.com/fil-forge/piri/pkg/config"
)

var (
	Cmd = &cobra.Command{
		Use:     "delegate",
		Aliases: []string{"dg"},
		Args:    cobra.NoArgs,
		Short:   `Operations for UCAN Delegations`,
	}

	GenerateCmd = &cobra.Command{
		Use:     "generate",
		Aliases: []string{"gen"},
		Args:    cobra.NoArgs,
		Short:   `Generate a new delegation`,
		RunE:    doGenerate,
	}
)

func init() {
	GenerateCmd.Flags().String(
		"client-did",
		"",
		"did of client delegation is for",
	)
	cobra.CheckErr(GenerateCmd.MarkFlagRequired("client-did"))

	GenerateCmd.Flags().String(
		"client-web-did",
		"",
		"web-did of client delegation is for, will cause delegation to wrap client did",
	)
	cobra.CheckErr(GenerateCmd.Flags().MarkHidden("client-web-did"))

	// The legacy --car flag selected a CAR-encoded archive output. UCAN 1.0
	// drops CAR archives in favor of DagCBOR container envelopes; the flag is
	// retained as a hidden no-op so existing callers don't error.
	GenerateCmd.Flags().Bool(
		"car",
		false,
		"[deprecated] no-op; UCAN 1.0 always emits a DagCBOR container",
	)
	cobra.CheckErr(GenerateCmd.Flags().MarkHidden("car"))

	Cmd.AddCommand(GenerateCmd)
}

func doGenerate(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load[config.Client]()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	var id ucan.Signer
	keySigner, err := lib.SignerFromEd25519PEMFile(cfg.Identity.KeyFile)
	if err != nil {
		return fmt.Errorf("parsing private key: %w", err)
	}
	id = keySigner

	if cmd.Flags().Changed("client-web-did") {
		cwd, err := cmd.Flags().GetString("client-web-did")
		if err != nil {
			return fmt.Errorf("getting --client-web-did flag: %w", err)
		}
		if !strings.HasPrefix(cwd, "did:web:") {
			return fmt.Errorf("issuer did:web: must start with 'did:web:' prefix")
		}
		issuerDidWeb, err := did.Parse(cwd)
		if err != nil {
			return fmt.Errorf("parsing issuer did web key (%s): %w", cwd, err)
		}
		wrapped, err := signer.Wrap(keySigner, issuerDidWeb)
		if err != nil {
			return fmt.Errorf("wrapping issuer with did web key (%s): %w", cwd, err)
		}
		id = wrapped
	}

	didStr, err := cmd.Flags().GetString("client-did")
	if err != nil {
		return fmt.Errorf("parsing --client-did flag: %w", err)
	}
	clientDid, err := did.Parse(didStr)
	if err != nil {
		return fmt.Errorf("parsing client-did: %w", err)
	}

	dlgs, err := MakeDelegations(
		id,
		clientDid,
		[]ucan.Command{
			blob.AllocateCommand,
			blob.AcceptCommand,
			pdp.InfoCommand,
			replica.AllocateCommand,
		},
		delegation.WithNoExpiration(),
	)
	if err != nil {
		return fmt.Errorf("creating delegation: %w", err)
	}

	envelope, err := EncodeDelegationsContainer(dlgs)
	if err != nil {
		return fmt.Errorf("encoding container: %w", err)
	}

	if cmd.Flags().Changed("car") {
		if _, err := io.Copy(os.Stdout, bytes.NewReader(envelope)); err != nil {
			return fmt.Errorf("writing delegation container to stdout: %w", err)
		}
		return nil
	}

	out, err := FormatDelegationBytes(envelope)
	if err != nil {
		return fmt.Errorf("formatting delegation as multibase-base64-encoded CIDv1: %w", err)
	}
	cmd.Println(out)
	return nil
}
