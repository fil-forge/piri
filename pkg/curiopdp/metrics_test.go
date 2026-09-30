package curiopdp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	chaintypes "github.com/filecoin-project/lotus/chain/types"
	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/fil-forge/piri/pkg/internal/testutil"
)

type fakeSource struct {
	sets    []proofSetSchedule
	setsErr error
	last    map[string]time.Time
	lastErr error
}

func (f *fakeSource) proofSetSchedules(context.Context) ([]proofSetSchedule, error) {
	return f.sets, f.setsErr
}

func (f *fakeSource) lastTaskSuccesses(context.Context) (map[string]time.Time, error) {
	return f.last, f.lastErr
}

func tipset(t *testing.T, height int64, timestamp uint64) *chaintypes.TipSet {
	t.Helper()
	c, err := cid.Decode("bafyreicmaj5hhoy5mgqvamfhgexxyergw7hdeshizghodwkjg6qmpoco7i")
	require.NoError(t, err)
	miner, err := address.NewIDAddress(1000)
	require.NoError(t, err)
	ts, err := chaintypes.NewTipSet([]*chaintypes.BlockHeader{{
		Miner:                 miner,
		Ticket:                &chaintypes.Ticket{VRFProof: []byte("ticket")},
		ElectionProof:         &chaintypes.ElectionProof{VRFProof: []byte("proof")},
		ParentWeight:          chaintypes.NewInt(0),
		ParentStateRoot:       c,
		ParentMessageReceipts: c,
		Messages:              c,
		Height:                abi.ChainEpoch(height),
		Timestamp:             timestamp,
		ParentBaseFee:         chaintypes.NewInt(100),
	}})
	require.NoError(t, err)
	return ts
}

func startTestMetrics(t *testing.T, head *chainHead, src pdpMetricsSource) *sdkmetric.ManualReader {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	require.NoError(t, startPDPMetrics(ctx, provider.Meter("test"), head, src))
	return reader
}

// gauges collects every int64 gauge as name -> attribute-set key -> value,
// the key being the single attribute's value ("" for none).
func gauges(t *testing.T, reader *sdkmetric.ManualReader) map[string]map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	out := map[string]map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			g, ok := m.Data.(metricdata.Gauge[int64])
			require.True(t, ok, "%s is not an int64 gauge", m.Name)
			for _, dp := range g.DataPoints {
				key := ""
				if kv := dp.Attributes.ToSlice(); len(kv) == 1 {
					key = kv[0].Value.AsString()
				} else {
					require.Empty(t, kv, "%s has more than one attribute", m.Name)
				}
				if out[m.Name] == nil {
					out[m.Name] = map[string]int64{}
				}
				out[m.Name][key] = dp.Value
			}
		}
	}
	return out
}

func ptr(v int64) *int64 { return &v }

func TestChainHeadGauges(t *testing.T) {
	head := &chainHead{}
	reader := startTestMetrics(t, head, &fakeSource{})

	got := gauges(t, reader)
	require.NotContains(t, got, "piri_chain_head_epoch", "no head observed before the first tipset")
	require.NotContains(t, got, "piri_chain_head_timestamp_seconds")

	require.NoError(t, head.update(context.Background(), nil, tipset(t, 100, 1_700_000_000)))
	got = gauges(t, reader)
	require.Equal(t, map[string]int64{"": 100}, got["piri_chain_head_epoch"])
	require.Equal(t, map[string]int64{"": 1_700_000_000}, got["piri_chain_head_timestamp_seconds"])

	// A reorg reports the applied side; a nil apply leaves the head alone.
	require.NoError(t, head.update(context.Background(), tipset(t, 101, 1_700_000_030), tipset(t, 102, 1_700_000_060)))
	require.NoError(t, head.update(context.Background(), nil, nil))
	got = gauges(t, reader)
	require.Equal(t, int64(102), got["piri_chain_head_epoch"][""])
	require.Equal(t, int64(1_700_000_060), got["piri_chain_head_timestamp_seconds"][""])
}

func TestProofSetAndTaskGauges(t *testing.T) {
	proveAt := time.Unix(1_700_000_500, 0)
	src := &fakeSource{
		sets: []proofSetSchedule{
			{ID: 7, ProveAtEpoch: 1000, ChallengeWindow: ptr(60), ProvingPeriod: ptr(2880)},
			{ID: 8, ProveAtEpoch: 2000}, // window and period unknown
		},
		last: map[string]time.Time{"PDPv0_Prove": proveAt},
	}
	reader := startTestMetrics(t, &chainHead{}, src)

	got := gauges(t, reader)
	require.Equal(t, map[string]int64{"7": 1000, "8": 2000}, got["piri_pdp_proofset_next_challenge_epoch"])
	require.Equal(t, map[string]int64{"7": 60}, got["piri_pdp_proofset_challenge_window_epochs"])
	require.Equal(t, map[string]int64{"7": 2880}, got["piri_pdp_proofset_proving_period_epochs"])
	require.Equal(t, map[string]int64{"PDPv0_Prove": proveAt.Unix()}, got["piri_pdp_task_last_success_timestamp_seconds"])
}

func TestGaugesSkipFailedReads(t *testing.T) {
	head := &chainHead{}
	require.NoError(t, head.update(context.Background(), nil, tipset(t, 5, 50)))
	src := &fakeSource{setsErr: errors.New("boom"), lastErr: errors.New("boom")}
	reader := startTestMetrics(t, head, src)

	got := gauges(t, reader)
	require.Equal(t, int64(5), got["piri_chain_head_epoch"][""], "head still reported when the database is not")
	require.NotContains(t, got, "piri_pdp_proofset_next_challenge_epoch")
	require.NotContains(t, got, "piri_pdp_task_last_success_timestamp_seconds")
}

func TestDBMetricsSource(t *testing.T) {
	db := testutil.NewHarmonyDB(t)
	ctx := context.Background()

	_, err := db.Exec(ctx, `INSERT INTO pdp_services (id, pubkey, service_label) VALUES (1, $1, 'storacha'), (2, $2, 'other')`, []byte{1}, []byte{2})
	require.NoError(t, err)
	_, err = db.Exec(ctx, `
		INSERT INTO pdp_data_sets (id, create_message_hash, service, prove_at_epoch, challenge_window, proving_period) VALUES
			(1, '0x1', 'storacha', 1000, 60, 2880),
			(2, '0x2', 'storacha', NULL, 60, 2880),
			(3, '0x3', 'other',    3000, 60, 2880),
			(4, '0x4', 'storacha', 4000, NULL, NULL)`)
	require.NoError(t, err)

	const host = "10.0.0.1:12300"
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	insertHistory := func(name string, end time.Time, ok bool, by string) {
		t.Helper()
		_, err := db.Exec(ctx, `
			INSERT INTO harmony_task_history (task_id, name, posted, work_start, work_end, result, completed_by_host_and_port)
			VALUES (1, $1, $2, $2, $2, $3, $4)`, name, end, ok, by)
		require.NoError(t, err)
	}
	insertHistory("PDPv0_Prove", t0, true, host)
	insertHistory("PDPv0_Prove", t0.Add(time.Hour), true, host)
	insertHistory("PDPv0_Prove", t0.Add(2*time.Hour), false, host)     // failed
	insertHistory("PDPv0_Prove", t0.Add(3*time.Hour), true, "other:1") // another machine
	insertHistory("PDPv0_InitPP", t0, true, host)
	insertHistory("PDPSync", t0.Add(4*time.Hour), true, host) // not exported

	src := newDBMetricsSource(db, pdpServiceLabel, host)
	now := t0.Add(5 * time.Hour)
	src.now = func() time.Time { return now }

	sets, err := src.proofSetSchedules(ctx)
	require.NoError(t, err)
	require.Equal(t, []proofSetSchedule{
		{ID: 1, ProveAtEpoch: 1000, ChallengeWindow: ptr(60), ProvingPeriod: ptr(2880)},
		{ID: 4, ProveAtEpoch: 4000},
	}, sets)

	last, err := src.lastTaskSuccesses(ctx)
	require.NoError(t, err)
	requireSameTimes(t, map[string]time.Time{
		"PDPv0_Prove":  t0.Add(time.Hour),
		"PDPv0_InitPP": t0,
	}, last)

	// The next read starts from the previous one, less the overlap; earlier
	// results are kept and a success within the overlap is still picked up.
	insertHistory("PDPv0_ProvPeriod", now.Add(-time.Minute), true, host)
	// A row that only becomes visible now but ended before the overlap is
	// missed: the overlap is what bounds how late a commit may land.
	insertHistory("PDPv0_Prove", t0.Add(90*time.Minute), true, host)
	now = now.Add(time.Minute)
	last, err = src.lastTaskSuccesses(ctx)
	require.NoError(t, err)
	requireSameTimes(t, map[string]time.Time{
		"PDPv0_Prove":      t0.Add(time.Hour),
		"PDPv0_InitPP":     t0,
		"PDPv0_ProvPeriod": t0.Add(5*time.Hour - time.Minute),
	}, last)
}

func requireSameTimes(t *testing.T, want, got map[string]time.Time) {
	t.Helper()
	require.Len(t, got, len(want))
	for k, w := range want {
		g, ok := got[k]
		require.True(t, ok, "missing %s", k)
		require.True(t, w.Equal(g), "%s: want %s, got %s", k, w, g)
	}
}
