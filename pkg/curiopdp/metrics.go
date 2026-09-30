package curiopdp

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/tasks/tasknames"
	chaintypes "github.com/filecoin-project/lotus/chain/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/fx"
)

// metricsQueryTimeout bounds each database read made while collecting, so a
// slow or unreachable database delays a collection instead of stalling it.
const metricsQueryTimeout = 5 * time.Second

// taskHistoryOverlap is how far before the previous read the next read of
// harmony_task_history starts. A row's work_end is taken before the insert
// commits, so a row can become visible after a read that started later than
// its work_end; the overlap re-reads that window. Taking the max again is
// harmless.
const taskHistoryOverlap = 10 * time.Minute

// provingTaskNames are the harmonytask names whose last success is exported.
var provingTaskNames = []string{
	tasknames.PDPv0_Prove,
	tasknames.PDPv0_ProvPeriod,
	tasknames.PDPv0_InitPP,
}

// chainHead holds the last tipset the chain scheduler applied. update runs
// synchronously in the scheduler's loop, so it only stores atomics.
type chainHead struct {
	seen      atomic.Bool
	epoch     atomic.Int64
	timestamp atomic.Int64 // unix seconds
}

func (h *chainHead) update(_ context.Context, _, apply *chaintypes.TipSet) error {
	if apply == nil {
		return nil
	}
	h.epoch.Store(int64(apply.Height()))
	h.timestamp.Store(int64(apply.MinTimestamp()))
	h.seen.Store(true)
	return nil
}

// load reports the last applied head, and false before the first one.
func (h *chainHead) load() (epoch, timestamp int64, ok bool) {
	if !h.seen.Load() {
		return 0, 0, false
	}
	return h.epoch.Load(), h.timestamp.Load(), true
}

// proofSetSchedule is one proof set's proving schedule from pdp_data_sets.
// ProveAtEpoch is never nil; rows without one are not returned.
type proofSetSchedule struct {
	ID              int64  `db:"id"`
	ProveAtEpoch    int64  `db:"prove_at_epoch"`
	ChallengeWindow *int64 `db:"challenge_window"`
	ProvingPeriod   *int64 `db:"proving_period"`
}

// pdpMetricsSource reads what the PDP gauges report from the database.
type pdpMetricsSource interface {
	// proofSetSchedules returns the schedule of every proof set this node
	// proves that has a next challenge epoch.
	proofSetSchedules(ctx context.Context) ([]proofSetSchedule, error)
	// lastTaskSuccesses returns, per task name, when a run of it last
	// completed successfully on this node. Names without a success are
	// absent.
	lastTaskSuccesses(ctx context.Context) (map[string]time.Time, error)
}

// dbMetricsSource is pdpMetricsSource over the harmony database.
type dbMetricsSource struct {
	db      *harmonydb.DB
	service string
	host    string

	mu sync.Mutex
	// since is where the next task history read starts; zero until the
	// first read succeeds.
	since time.Time
	// last accumulates the latest success per task name across reads.
	last map[string]time.Time
	now  func() time.Time
}

func newDBMetricsSource(db *harmonydb.DB, service, host string) *dbMetricsSource {
	return &dbMetricsSource{
		db:      db,
		service: service,
		host:    host,
		last:    map[string]time.Time{},
		now:     time.Now,
	}
}

func (s *dbMetricsSource) proofSetSchedules(ctx context.Context) ([]proofSetSchedule, error) {
	var rows []proofSetSchedule
	if err := s.db.Select(ctx, &rows, `
		SELECT id, prove_at_epoch, challenge_window, proving_period
		FROM pdp_data_sets
		WHERE service = $1 AND prove_at_epoch IS NOT NULL
		ORDER BY id`, s.service); err != nil {
		return nil, fmt.Errorf("reading proof set schedules: %w", err)
	}
	return rows, nil
}

// lastTaskSuccesses reads harmony_task_history incrementally: the table is
// never pruned, so rather than take the max over all of it on every
// collection, each read only covers rows ending after the previous read
// started (less taskHistoryOverlap), and the results accumulate.
func (s *dbMetricsSource) lastTaskSuccesses(ctx context.Context) (map[string]time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	started := s.now()
	var rows []struct {
		Name    string    `db:"name"`
		WorkEnd time.Time `db:"work_end"`
	}
	if err := s.db.Select(ctx, &rows, `
		SELECT name, MAX(work_end) AS work_end
		FROM harmony_task_history
		WHERE result = TRUE
		  AND completed_by_host_and_port = $1
		  AND name = ANY($2)
		  AND work_end > $3
		GROUP BY name`, s.host, provingTaskNames, s.since.UTC()); err != nil {
		return nil, fmt.Errorf("reading task history: %w", err)
	}
	for _, r := range rows {
		if r.WorkEnd.After(s.last[r.Name]) {
			s.last[r.Name] = r.WorkEnd
		}
	}
	s.since = started.Add(-taskHistoryOverlap)

	out := make(map[string]time.Time, len(s.last))
	for k, v := range s.last {
		out[k] = v
	}
	return out, nil
}

// startPDPMetrics exports the chain head the PDP pipeline last saw and the
// proving schedule and progress of this node's proof sets. Together they let
// an alert tell a node whose chain view has stopped advancing, or that has
// stopped proving, from a healthy one; no fault is computed here.
//
// head is fed by a chain scheduler handler (see registerPDPMetrics); nothing
// is observed for it until the first tipset arrives. The proof set and task
// gauges are read from src on each collection; a failed read is logged and
// that collection skips those gauges.
func startPDPMetrics(ctx context.Context, meter metric.Meter, head *chainHead, src pdpMetricsSource) error {
	headEpoch, err := meter.Int64ObservableGauge(
		"piri_chain_head_epoch",
		metric.WithDescription("Epoch of the last tipset the PDP chain scheduler applied"),
		metric.WithUnit("{epoch}"),
	)
	if err != nil {
		return fmt.Errorf("create chain head epoch gauge: %w", err)
	}
	headTimestamp, err := meter.Int64ObservableGauge(
		"piri_chain_head_timestamp_seconds",
		metric.WithDescription("Timestamp (unix seconds) of the last tipset the PDP chain scheduler applied"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("create chain head timestamp gauge: %w", err)
	}
	nextChallenge, err := meter.Int64ObservableGauge(
		"piri_pdp_proofset_next_challenge_epoch",
		metric.WithDescription("Epoch at which the proof set's next challenge window opens (pdp_data_sets.prove_at_epoch)"),
		metric.WithUnit("{epoch}"),
	)
	if err != nil {
		return fmt.Errorf("create next challenge epoch gauge: %w", err)
	}
	challengeWindow, err := meter.Int64ObservableGauge(
		"piri_pdp_proofset_challenge_window_epochs",
		metric.WithDescription("Length of the proof set's challenge window"),
		metric.WithUnit("{epoch}"),
	)
	if err != nil {
		return fmt.Errorf("create challenge window gauge: %w", err)
	}
	provingPeriod, err := meter.Int64ObservableGauge(
		"piri_pdp_proofset_proving_period_epochs",
		metric.WithDescription("Length of the proof set's proving period"),
		metric.WithUnit("{epoch}"),
	)
	if err != nil {
		return fmt.Errorf("create proving period gauge: %w", err)
	}
	taskLastSuccess, err := meter.Int64ObservableGauge(
		"piri_pdp_task_last_success_timestamp_seconds",
		metric.WithDescription("When a PDP task last completed successfully on this node (unix seconds)"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("create task last success gauge: %w", err)
	}

	reg, err := meter.RegisterCallback(
		func(ctx context.Context, o metric.Observer) error {
			if epoch, ts, ok := head.load(); ok {
				o.ObserveInt64(headEpoch, epoch)
				o.ObserveInt64(headTimestamp, ts)
			}

			qctx, cancel := context.WithTimeout(ctx, metricsQueryTimeout)
			defer cancel()

			if sets, err := src.proofSetSchedules(qctx); err != nil {
				log.Warnw("measuring proof set schedules", "error", err)
			} else {
				for _, ps := range sets {
					attrs := metric.WithAttributes(attribute.String("proof_set", strconv.FormatInt(ps.ID, 10)))
					o.ObserveInt64(nextChallenge, ps.ProveAtEpoch, attrs)
					if ps.ChallengeWindow != nil {
						o.ObserveInt64(challengeWindow, *ps.ChallengeWindow, attrs)
					}
					if ps.ProvingPeriod != nil {
						o.ObserveInt64(provingPeriod, *ps.ProvingPeriod, attrs)
					}
				}
			}

			if last, err := src.lastTaskSuccesses(qctx); err != nil {
				log.Warnw("measuring PDP task history", "error", err)
			} else {
				for name, at := range last {
					o.ObserveInt64(taskLastSuccess, at.Unix(), metric.WithAttributes(attribute.String("task_name", name)))
				}
			}
			return nil
		},
		headEpoch,
		headTimestamp,
		nextChallenge,
		challengeWindow,
		provingPeriod,
		taskLastSuccess,
	)
	if err != nil {
		return fmt.Errorf("register PDP metrics callback: %w", err)
	}

	go func() {
		<-ctx.Done()
		if err := reg.Unregister(); err != nil {
			log.Warnw("failed to unregister PDP metrics callback", "error", err)
		}
	}()
	return nil
}

// registerPDPMetrics hooks the chain head tracker into the chain scheduler
// (which must happen before startPipeline runs it, hence at construction) and
// exports the PDP metrics for the life of the app on the global meter the
// serve command configures before wiring. Task history is filtered to the
// engine's own host-and-port, which is how harmonytask records the node that
// completed a task.
func registerPDPMetrics(lc fx.Lifecycle, db *harmonydb.DB, cs *chainsched.CurioChainSched, eng *harmonytask.TaskEngine) error {
	head := &chainHead{}
	if err := cs.AddHandler(head.update); err != nil {
		return fmt.Errorf("registering chain head metrics handler: %w", err)
	}
	src := newDBMetricsSource(db, pdpServiceLabel, eng.Host())

	ctx, cancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			meter := otel.GetMeterProvider().Meter("github.com/fil-forge/piri/pkg/curiopdp")
			return startPDPMetrics(ctx, meter, head, src)
		},
		OnStop: func(context.Context) error {
			cancel()
			return nil
		},
	})
	return nil
}
