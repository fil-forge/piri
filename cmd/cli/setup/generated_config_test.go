package setup

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/config"
	appcfg "github.com/fil-forge/piri/pkg/config/app"
)

var updateGeneratedConfig = flag.Bool("update", false,
	"record the golden generated config for a new GeneratedConfigVersion")

// TestGeneratedConfig pins what `piri init` writes for a fixed base config and
// fixed flags, keyed by config.GeneratedConfigVersion. Deployments re-run init
// when that version changes and not otherwise, so init's output must not change
// under an unchanged version: this test fails when it does.
//
// To change init's output on purpose, bump config.GeneratedConfigVersion,
// delete the old golden file, and run
//
//	go test -tags skiff ./cmd/cli/setup -run TestGeneratedConfig -update
//
// -update only writes a golden file that does not exist yet, so it cannot be
// used to paper over a change without a bump.
func TestGeneratedConfig(t *testing.T) {
	dir := filepath.Join("testdata", "generated-config")

	baseValues, err := loadBaseConfig(filepath.Join(dir, "base-config.toml"))
	require.NoError(t, err)

	publicURL, err := url.Parse("https://piri.example.com")
	require.NoError(t, err)
	flags := &initFlags{
		keyFile:        "/keys/piri.pem",
		publicURL:      publicURL,
		lotusEndpoint:  "wss://lotus.example.com/rpc/v1",
		lotusAuthToken: "",
		plcDirectory:   "https://plc.example.com",
		baseConfig:     baseValues,
		storage: &storageConfig{
			database: baseValues.database,
			s3:       baseValues.s3Config,
		},
	}
	cfg := &appcfg.AppConfig{
		Storage: appcfg.StorageConfig{DataDir: "/data/piri", TempDir: "/tmp/piri"},
		Server:  appcfg.ServerConfig{Host: "0.0.0.0", Port: 3000},
	}

	generated, err := generateConfig(cfg, flags,
		common.HexToAddress("0x9012345678901234567890123456789012345678"),
		42, "indexer-proof", "egress-proof")
	require.NoError(t, err)

	// The same encoding init uses to write the file.
	got, err := toml.Marshal(generated)
	require.NoError(t, err)

	golden := filepath.Join(dir, fmt.Sprintf("v%d.toml", config.GeneratedConfigVersion))
	want, err := os.ReadFile(golden)
	if os.IsNotExist(err) {
		if *updateGeneratedConfig {
			require.NoError(t, os.WriteFile(golden, got, 0o644))
			t.Logf("recorded %s", golden)
			return
		}
		t.Fatalf("no golden config for GeneratedConfigVersion %d at %s; run with -update to record it",
			config.GeneratedConfigVersion, golden)
	}
	require.NoError(t, err)

	require.Equalf(t, string(want), string(got),
		"piri init's output changed for the same inputs, but GeneratedConfigVersion is still %d. "+
			"If the change is intended, bump config.GeneratedConfigVersion so deployments re-run init, "+
			"delete %s, and record the new one with -update.",
		config.GeneratedConfigVersion, golden)
}
