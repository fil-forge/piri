package cliutil

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"path"

	"github.com/fil-forge/go-ucanto/core/delegation"
	"github.com/fil-forge/go-ucanto/did"
	"github.com/labstack/gommon/color"
	"github.com/multiformats/go-multiaddr"
	"github.com/spf13/cobra"

	"github.com/fil-forge/piri/pkg/build"
)

func PrintHero(w io.Writer, id did.DID) {
	fmt.Fprintf(w, `
▗▄▄▖ ▄  ▄▄▄ ▄  %s
▐▌ ▐▌▄ █    ▄  %s
▐▛▀▘ █ █    █  %s
▐▌   █      █  %s

🔥 %s
🆔 %s
🚀 Ready!
`,
		color.Green(" ▗"),
		color.Red(" █")+color.Red("▌", color.D),
		color.Red("▗", color.B)+color.Red("█")+color.Red("▘", color.D),
		color.Red("▀")+color.Red("▘", color.D),
		build.Version, id.String())
}

func Mkdirp(dirpath ...string) (string, error) {
	dir := path.Join(dirpath...)
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return "", fmt.Errorf("creating directory: %s: %w", dir, err)
	}
	return dir, nil
}

type UCANServerConfig struct {
	Host                 string
	Port                 uint
	DataDir              string
	PublicURL            *url.URL
	BlobAddr             multiaddr.Multiaddr
	IndexingServiceDID   did.DID
	IndexingServiceURL   *url.URL
	IndexingServiceProof delegation.Proof
	UploadServiceDID     did.DID
	UploadServiceURL     *url.URL
	IPNIAnnounceURLs     []url.URL
}

func PrintUCANServerConfig(cmd *cobra.Command, cfg UCANServerConfig) {
	cmd.Println("SERVER CONFIGURATION")
	cmd.Println("--------------------")
	cmd.Printf("Host:        %s\n", cfg.Host)
	cmd.Printf("Port:        %d\n", cfg.Port)
	cmd.Printf("Data Dir:    %s\n", cfg.DataDir)
	cmd.Printf("Public URL:  %s\n", cfg.PublicURL)
	if cfg.BlobAddr != nil {
		cmd.Printf("Blob Addr:   %s\n", cfg.BlobAddr)
	}

	cmd.Println()
	cmd.Println("SERVICES")
	cmd.Println("--------")
	cmd.Println("Indexing Service:")
	cmd.Printf("  DID:       %s\n", cfg.IndexingServiceDID)
	cmd.Printf("  URL:       %s\n", cfg.IndexingServiceURL)
	cmd.Printf("  Proof Set: %t\n", cfg.IndexingServiceProof != delegation.Proof{})
	cmd.Println()
	cmd.Println("Upload Service:")
	cmd.Printf("  DID:       %s\n", cfg.UploadServiceDID)
	cmd.Printf("  URL:       %s\n", cfg.UploadServiceURL)

	cmd.Println()
	cmd.Println("IPNI ANNOUNCE URLS")
	cmd.Println("------------------")
	if len(cfg.IPNIAnnounceURLs) == 0 {
		cmd.Println("  (none configured)")
	} else {
		for _, url := range cfg.IPNIAnnounceURLs {
			cmd.Printf("  • %s\n", url.String())
		}
	}

	cmd.Println()
}
