package curiopdp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/curiostorage/harmonyquery"
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
	sets    []proofSetState
	setsErr error
	last    map[string]time.Time
	lastErr error
	pool    harmonyquery.PoolStat
}

func (f *fakeSource) poolStat() harmonyquery.PoolStat {
	return f.pool
}

func (f *fakeSource) proofSets(context.Context) ([]proofSetState, error) {
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
// the key being the single attribute's value ("" for none). Other instrument
// kinds (the pool counters) are left out; sums reads those.
func gauges(t *testing.T, reader *sdkmetric.ManualReader) map[string]map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	out := map[string]map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			g, ok := m.Data.(metricdata.Gauge[int64])
			if !ok {
				continue
			}
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

// sums collects every monotonic sum, int64 or float64, as name -> value.
// Each pool counter has one data point and no attributes.
func sums(t *testing.T, reader *sdkmetric.ManualReader) map[string]float64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	out := map[string]float64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch d := m.Data.(type) {
			case metricdata.Sum[int64]:
				require.True(t, d.IsMonotonic, "%s is not monotonic", m.Name)
				require.Len(t, d.DataPoints, 1, "%s has more than one data point", m.Name)
				require.Empty(t, d.DataPoints[0].Attributes.ToSlice(), "%s has attributes", m.Name)
				out[m.Name] = float64(d.DataPoints[0].Value)
			case metricdata.Sum[float64]:
				require.True(t, d.IsMonotonic, "%s is not monotonic", m.Name)
				require.Len(t, d.DataPoints, 1, "%s has more than one data point", m.Name)
				require.Empty(t, d.DataPoints[0].Attributes.ToSlice(), "%s has attributes", m.Name)
				out[m.Name] = d.DataPoints[0].Value
			}
		}
	}
	return out
}

func TestPoolMetrics(t *testing.T) {
	src := &fakeSource{pool: harmonyquery.PoolStat{
		AcquireCount:         1200,
		AcquireDuration:      3 * time.Second,
		EmptyAcquireCount:    40,
		EmptyAcquireWaitTime: 2500 * time.Millisecond,
		CanceledAcquireCount: 2,
		AcquiredConns:        3,
		IdleConns:            5,
		TotalConns:           8,
		MaxConns:             16,
	}}
	reader := startTestMetrics(t, &chainHead{}, src)

	got := gauges(t, reader)
	require.Equal(t, map[string]int64{"acquired": 3, "idle": 5}, got["piri_pdp_db_pool_connections"])
	require.Equal(t, map[string]int64{"": 16}, got["piri_pdp_db_pool_max_connections"])

	require.Equal(t, map[string]float64{
		"piri_pdp_db_pool_acquires":             1200,
		"piri_pdp_db_pool_acquire_seconds":      3,
		"piri_pdp_db_pool_acquires_waited":      40,
		"piri_pdp_db_pool_acquire_wait_seconds": 2.5,
		"piri_pdp_db_pool_acquires_cancelled":   2,
	}, sums(t, reader))

	// Counters follow the pool; a second collection reports the new totals.
	src.pool.AcquireCount = 1300
	src.pool.EmptyAcquireCount = 41
	require.Equal(t, float64(1300), sums(t, reader)["piri_pdp_db_pool_acquires"])
	require.Equal(t, float64(41), sums(t, reader)["piri_pdp_db_pool_acquires_waited"])
}

func TestPoolMetricsReportedWhenDatabaseReadsFail(t *testing.T) {
	src := &fakeSource{
		setsErr: errors.New("boom"), lastErr: errors.New("boom"),
		pool: harmonyquery.PoolStat{MaxConns: 4},
	}
	reader := startTestMetrics(t, &chainHead{}, src)
	require.Equal(t, map[string]int64{"": 4}, gauges(t, reader)["piri_pdp_db_pool_max_connections"])
}

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
	drainQueued := time.Unix(1_700_000_200, 0)
	src := &fakeSource{
		sets: []proofSetState{
			{ID: 7, ProveAtEpoch: ptr(1000), ChallengeWindow: ptr(60), ProvingPeriod: ptr(2880)},
			// 8: window and period unknown; 9: no schedule, drain queued; 10 and 11: unrecoverable.
			{ID: 8, ProveAtEpoch: ptr(2000)},
			{ID: 9, ChallengeWindow: ptr(60), ProvingPeriod: ptr(2880), DrainQueuedAt: &drainQueued},
			{ID: 10, ChallengeWindow: ptr(60), ProvingPeriod: ptr(2880), UnrecoverableEpoch: ptr(400), DrainQueuedAt: &drainQueued},
			{ID: 11, UnrecoverableEpoch: ptr(450)},
		},
		last: map[string]time.Time{"PDPv0_Prove": proveAt},
	}
	reader := startTestMetrics(t, &chainHead{}, src)

	got := gauges(t, reader)
	require.Equal(t, map[string]int64{"7": 1000, "8": 2000}, got["piri_pdp_proofset_next_challenge_epoch"])
	require.Equal(t, map[string]int64{"7": 60}, got["piri_pdp_proofset_challenge_window_epochs"])
	require.Equal(t, map[string]int64{"7": 2880}, got["piri_pdp_proofset_proving_period_epochs"])
	// Unrecoverable proof sets are counted, not reported per proof set.
	require.Equal(t, map[string]int64{"": 2}, got["piri_pdp_proofsets_unrecoverable"])
	// A queued drain is reported whether or not a schedule is set.
	require.Equal(t, map[string]int64{"9": drainQueued.Unix()}, got["piri_pdp_proofset_deletion_drain_queued_timestamp_seconds"])
	require.Equal(t, map[string]int64{"PDPv0_Prove": proveAt.Unix()}, got["piri_pdp_task_last_success_timestamp_seconds"])
}

func TestUnrecoverableReportedWhenZero(t *testing.T) {
	reader := startTestMetrics(t, &chainHead{}, &fakeSource{
		sets: []proofSetState{{ID: 1, ProveAtEpoch: ptr(1000)}},
	})
	got := gauges(t, reader)
	require.Equal(t, map[string]int64{"": 0}, got["piri_pdp_proofsets_unrecoverable"])
}

func TestGaugesSkipFailedReads(t *testing.T) {
	head := &chainHead{}
	require.NoError(t, head.update(context.Background(), nil, tipset(t, 5, 50)))
	src := &fakeSource{setsErr: errors.New("boom"), lastErr: errors.New("boom")}
	reader := startTestMetrics(t, head, src)

	got := gauges(t, reader)
	require.Equal(t, int64(5), got["piri_chain_head_epoch"][""], "head still reported when the database is not")
	require.NotContains(t, got, "piri_pdp_proofset_next_challenge_epoch")
	require.NotContains(t, got, "piri_pdp_proofsets_unrecoverable")
	require.NotContains(t, got, "piri_pdp_task_last_success_timestamp_seconds")
}

func TestDBMetricsSource(t *testing.T) {
	db := testutil.NewHarmonyDB(t)
	ctx := context.Background()

	_, err := db.Exec(ctx, `INSERT INTO pdp_services (id, pubkey, service_label) VALUES (1, $1, 'storacha'), (2, $2, 'other')`, []byte{1}, []byte{2})
	require.NoError(t, err)
	_, err = db.Exec(ctx, `
		INSERT INTO pdp_data_sets (id, create_message_hash, service, prove_at_epoch, challenge_window, proving_period,
		                           unrecoverable_proving_failure_epoch) VALUES
			(1, '0x1', 'storacha', 1000, 60, 2880, NULL),
			(2, '0x2', 'storacha', NULL, 60, 2880, 900),
			(3, '0x3', 'other',    3000, 60, 2880, NULL),
			(4, '0x4', 'storacha', 4000, NULL, NULL, NULL)`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO pdpv0_deletion_drain (data_set, created_at) VALUES (4, '2026-01-01T00:30:00Z')`)
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
	insertHistory("PDPv0_ProcDel", t0, true, host)
	insertHistory("PDPSync", t0.Add(4*time.Hour), true, host) // not exported

	src := newDBMetricsSource(db, pdpServiceLabel, host)
	now := t0.Add(5 * time.Hour)
	src.now = func() time.Time { return now }

	sets, err := src.proofSets(ctx)
	require.NoError(t, err)
	for i := range sets {
		if q := sets[i].DrainQueuedAt; q != nil {
			utc := q.UTC() // compare instants, not the location pgx decodes into
			sets[i].DrainQueuedAt = &utc
		}
	}
	drainQueued := t0.Add(30 * time.Minute)
	require.Equal(t, []proofSetState{
		{ID: 1, ProveAtEpoch: ptr(1000), ChallengeWindow: ptr(60), ProvingPeriod: ptr(2880)},
		{ID: 2, ChallengeWindow: ptr(60), ProvingPeriod: ptr(2880), UnrecoverableEpoch: ptr(900)},
		{ID: 4, ProveAtEpoch: ptr(4000), DrainQueuedAt: &drainQueued},
	}, sets)

	last, err := src.lastTaskSuccesses(ctx)
	require.NoError(t, err)
	requireSameTimes(t, map[string]time.Time{
		"PDPv0_Prove":   t0.Add(time.Hour),
		"PDPv0_InitPP":  t0,
		"PDPv0_ProcDel": t0,
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
		"PDPv0_ProcDel":    t0,
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
