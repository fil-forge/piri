package setup

import (
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/ethereum/go-ethereum/common"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/config"
	appcfg "github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/presets"
)

var updateGeneratedConfig = flag.Bool("update", false,
	"record the golden generated configs for a new GeneratedConfigVersion")

// goldenWorkers stands in for the per-CPU worker counts init writes. See
// pinCPUWorkers.
const goldenWorkers = 4

// TestGeneratedConfig pins what `piri init` writes for fixed inputs, keyed by
// config.GeneratedConfigVersion. Deployments re-run init when that version
// changes and not otherwise, so init's output must not change under an
// unchanged version: this test fails when it does.
//
// It covers --base-config, with a fixture that sets every key serve reads, and
// every --network preset. A change to a preset's values changes init's output
// too, so it also needs a bump.
//
// Flags go through init's own flag registration and parseAndValidateFlags.
// The node setup, contract registration and delegator steps need a chain and
// the network, so their results (owner address, proof set, proofs) are fixed
// values here.
//
// To change init's output on purpose, bump config.GeneratedConfigVersion,
// delete the old golden files, and run
//
//	go test -tags skiff ./cmd/cli/setup -run TestGeneratedConfig -update
//
// -update only writes golden files that do not exist yet, so it cannot be used
// to paper over a change without a bump.
func TestGeneratedConfig(t *testing.T) {
	dir := filepath.Join("testdata", "generated-config")
	baseConfig := filepath.Join(dir, "base-config.toml")
	t.Setenv("PIRI_PDP_LOTUS_AUTH_TOKEN", "fixture-lotus-token")

	type goldenCase struct {
		name string
		args []string
	}
	// The base config supplies storage settings, so the presets take them from
	// flags instead.
	cases := []goldenCase{{
		name: "base-config",
		args: []string{"--base-config=" + baseConfig, "--registrar-url=https://registrar.example.com"},
	}}
	for _, network := range presets.AvailableNetworks {
		cases = append(cases, goldenCase{
			name: string(network),
			args: []string{
				"--network=" + string(network),
				"--db-type=postgres",
				"--db-postgres-url=postgres://piri:fixture-db-password@db.example.com:5432/piri",
				"--db-postgres-max-open-conns=21",
				"--db-postgres-max-idle-conns=19",
				"--db-postgres-conn-max-lifetime=50m",
				"--s3-endpoint=s3.example.com:9000",
				"--s3-bucket-prefix=fixture-",
				"--s3-access-key-id=fixture-access-key",
				"--s3-secret-access-key=fixture-secret-key",
				"--s3-insecure",
			},
		})
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := generateGoldenConfig(t, tc.args)

			golden := filepath.Join(dir, goldenName(tc.name))
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
					"delete the golden files for the old version, and record new ones with -update.",
				config.GeneratedConfigVersion)
		})
	}
}

// goldenCommonArgs are the init flags every golden case passes.
var goldenCommonArgs = []string{
	"--data-dir=/data/piri",
	"--temp-dir=/tmp/piri",
	"--key-file=/keys/piri.pem",
	"--wallet-file=/keys/wallet.hex",
	"--lotus-endpoint=wss://lotus.example.com/rpc/v1",
	"--operator-email=operator@example.com",
	"--public-url=https://piri.example.com",
	"--host=0.0.0.0",
	"--port=3000",
	"--plc-directory=https://plc.example.com",
}

// generateGoldenConfig runs init's flag parsing and config generation with
// goldenCommonArgs plus args, and returns the config as init would write it.
func generateGoldenConfig(t *testing.T, args []string) []byte {
	t.Helper()
	cmd := &cobra.Command{Use: "init"}
	addInitFlags(cmd)
	require.NoError(t, cmd.ParseFlags(append(append([]string{}, goldenCommonArgs...), args...)))
	require.NoError(t, cmd.ValidateRequiredFlags())
	require.NoError(t, cmd.ValidateFlagGroups())

	flags, err := parseAndValidateFlags(cmd)
	require.NoError(t, err)

	// createNode copies these from the flags unchanged; the rest of what it
	// builds is not read by generateConfig.
	cfg := &appcfg.AppConfig{
		Storage: appcfg.StorageConfig{DataDir: flags.dataDir, TempDir: flags.tempDir},
		Server:  appcfg.ServerConfig{Host: flags.host, Port: flags.port},
	}
	generated, err := generateConfig(cfg, flags,
		common.HexToAddress("0x9012345678901234567890123456789012345678"),
		42, "indexer-proof", "egress-proof")
	require.NoError(t, err)
	pinCPUWorkers(&generated)

	// The same encoding init uses to write the file.
	got, err := toml.Marshal(generated)
	require.NoError(t, err)
	return got
}

// goldenName is the golden file for one case at the current version.
func goldenName(name string) string {
	if name == "base-config" {
		return fmt.Sprintf("v%d.toml", config.GeneratedConfigVersion)
	}
	return fmt.Sprintf("v%d-%s.toml", config.GeneratedConfigVersion, name)
}

// pinCPUWorkers replaces worker counts that come from the CPU count of the
// machine running init, so the golden files do not depend on the machine
// running the test. A count init takes from anywhere else is left alone and
// shows up in the comparison.
func pinCPUWorkers(cfg *config.FullServerConfig) {
	defaults := config.DefaultAggregationConfig()
	aggregation := &cfg.PDPService.Aggregation
	for _, p := range []struct {
		got    *uint
		perCPU uint
	}{
		{&aggregation.CommP.JobQueue.Workers, defaults.CommP.JobQueue.Workers},
		{&aggregation.Aggregator.JobQueue.Workers, defaults.Aggregator.JobQueue.Workers},
	} {
		if *p.got == p.perCPU {
			*p.got = goldenWorkers
		}
	}
}

// TestGeneratedConfigFixtureIsComplete checks that the base-config fixture
// sets every key of the server config and nothing either config ignores, so a
// section init starts honouring is already in the fixture. It also checks that
// each fixture value differs from what init writes without it, so honouring it
// shows up in the golden file.
func TestGeneratedConfigFixtureIsComplete(t *testing.T) {
	path := filepath.Join("testdata", "generated-config", "base-config.toml")

	var full config.FullServerConfig
	fullMeta, err := toml.DecodeFile(path, &full)
	require.NoError(t, err)
	var base baseConfig
	baseMeta, err := toml.DecodeFile(path, &base)
	require.NoError(t, err)

	// IsDefined does not see into arrays of tables, so compare key paths.
	defined := map[string]bool{}
	for _, key := range fullMeta.Keys() {
		defined[key.String()] = true
	}
	// Serve decodes the config with mapstructure, init writes it with toml, so
	// the two names must agree for serve to read what init writes.
	sameMapstructureName := func(key []string, field reflect.StructField) {
		name, _, _ := strings.Cut(field.Tag.Get("mapstructure"), ",")
		if name == "" {
			name = field.Name
		}
		require.Equalf(t, key[len(key)-1], name,
			"%s: mapstructure name differs from toml name", strings.Join(key, "."))
	}
	for _, key := range tomlLeafKeys(reflect.TypeOf(full), nil, sameMapstructureName) {
		require.Truef(t, defined[strings.Join(key, ".")],
			"%s does not set %s; set it to a value init would not write on its own", path, strings.Join(key, "."))
	}
	for _, key := range tomlLeafKeys(reflect.TypeOf(base), nil, nil) {
		require.Truef(t, defined[strings.Join(key, ".")],
			"%s does not set %s, which init's base config parses", path, strings.Join(key, "."))
	}

	unknown := map[string]bool{}
	for _, key := range baseMeta.Undecoded() {
		unknown[key.String()] = true
	}
	for _, key := range fullMeta.Undecoded() {
		require.Falsef(t, unknown[key.String()],
			"%s sets %s, which neither serve nor init reads", path, key)
	}

	// What init writes from an empty base config: every key absent at once.
	empty := filepath.Join(t.TempDir(), "empty.toml")
	require.NoError(t, os.WriteFile(empty, nil, 0o600))
	t.Setenv("PIRI_PDP_LOTUS_AUTH_TOKEN", "fixture-lotus-token")
	var fixture, absent map[string]any
	_, err = toml.DecodeFile(path, &fixture)
	require.NoError(t, err)
	_, err = toml.Decode(string(generateGoldenConfig(t, []string{
		"--base-config=" + empty, "--registrar-url=https://registrar.example.com",
	})), &absent)
	require.NoError(t, err)
	fixtureValues, absentValues := tomlLeafValues(fixture, ""), tomlLeafValues(absent, "")
	for key, value := range fixtureValues {
		require.NotEqualf(t, absentValues[key], value,
			"%s sets %s to what init writes without it; pick another value", path, key)
	}
}

// tomlLeafKeys lists the TOML key paths of every non-table field in t, calling
// check, if set, on each field along the way.
func tomlLeafKeys(t reflect.Type, prefix []string, check func(key []string, field reflect.StructField)) [][]string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Struct {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return [][]string{prefix}
	}
	var keys [][]string
	for i := range t.NumField() {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("toml"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		key := append(append([]string{}, prefix...), name)
		if check != nil {
			check(key, field)
		}
		keys = append(keys, tomlLeafKeys(field.Type, key, check)...)
	}
	return keys
}

// tomlLeafValues maps the dotted path of every non-table value in a decoded
// TOML document to its value. Values under an array of tables are collected
// into a slice per path.
func tomlLeafValues(doc map[string]any, prefix string) map[string]any {
	values := map[string]any{}
	for name, value := range doc {
		key := name
		if prefix != "" {
			key = prefix + "." + name
		}
		switch v := value.(type) {
		case map[string]any:
			maps.Copy(values, tomlLeafValues(v, key))
		case []map[string]any:
			for _, table := range v {
				for k, leaf := range tomlLeafValues(table, key) {
					prev, _ := values[k].([]any)
					values[k] = append(prev, leaf)
				}
			}
		default:
			values[key] = value
		}
	}
	return values
}
