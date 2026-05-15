package egresstracker

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/ipfs/go-cid"

	"github.com/fil-forge/libforge/capabilities"
	"github.com/fil-forge/libforge/capabilities/space/egress"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/execution"
	"github.com/fil-forge/ucantone/principal"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/fil-forge/ucantone/ucan/receipt"

	"github.com/fil-forge/piri/pkg/client/receipts"
	"github.com/fil-forge/piri/pkg/store/consolidationstore"
	"github.com/fil-forge/piri/pkg/store/consolidationstore/consolidation"
	"github.com/fil-forge/piri/pkg/store/local/retrievaljournal"
)

const journalRotationPeriod = time.Hour * 12

// ErrNotMigrated indicates the egress tracker's outbound RPC is disabled
// because no service connection has been configured. Journaling, cleanup,
// and queue retry still run; only the outbound send is short-circuited.
var ErrNotMigrated = errors.New("egress tracker outbound RPC disabled: no egress tracker service connection configured")

// Service stores receipts from /content/retrieve invocations, batches them
// and sends them to an egress tracking service via a /space/egress/track
// invocation. When the service connection is not configured, the outbound
// send returns ErrNotMigrated; reception, journaling, and cleanup still run.
type Service struct {
	id                   principal.Signer
	egressTrackerDID     did.DID
	egressTrackerConn    execution.Executor
	egressTrackerProofs  []cid.Cid
	batchEndpoint        *url.URL
	journal              retrievaljournal.Journal
	journalRotator       *retrievaljournal.PeriodicRotator
	queue                EgressTrackerQueue
	consolidationStore   consolidationstore.Store
	rcptsClient          *receipts.Client
	cleanupCheckInterval time.Duration
	cleanupCancel        context.CancelFunc
	cleanupDone          chan struct{}
}

func New(
	id principal.Signer,
	egressTrackerDID did.DID,
	egressTrackerConn execution.Executor,
	egressTrackerProofs []cid.Cid,
	batchEndpoint *url.URL,
	journal retrievaljournal.Journal,
	consolidationStore consolidationstore.Store,
	queue EgressTrackerQueue,
	rcptsClient *receipts.Client,
	cleanupCheckInterval time.Duration,
) (*Service, error) {
	var journalRotator *retrievaljournal.PeriodicRotator
	if fr, ok := journal.(retrievaljournal.ForceRotator); ok {
		journalRotator = retrievaljournal.NewPeriodicRotator(fr, journalRotationPeriod)
	}

	svc := &Service{
		id:                   id,
		egressTrackerDID:     egressTrackerDID,
		egressTrackerConn:    egressTrackerConn,
		egressTrackerProofs:  egressTrackerProofs,
		batchEndpoint:        batchEndpoint,
		journal:              journal,
		journalRotator:       journalRotator,
		consolidationStore:   consolidationStore,
		queue:                queue,
		rcptsClient:          rcptsClient,
		cleanupCheckInterval: cleanupCheckInterval,
		cleanupDone:          make(chan struct{}),
	}

	if err := queue.Register(svc.egressTrack); err != nil {
		return nil, fmt.Errorf("registering egress track task: %w", err)
	}

	if journalRotator != nil {
		journalRotator.RotateFunc = func(batchID cid.Cid) {
			if err := svc.enqueueEgressTrackTask(context.Background(), batchID); err != nil {
				log.Errorw("enqueuing egress track task", "batch", batchID, "error", err)
			}
		}
	}

	return svc, nil
}

func (s *Service) AddReceipt(ctx context.Context, rcpt *receipt.Receipt) error {
	batchRotated, rotatedBatchCID, err := s.journal.Append(ctx, rcpt)
	if err != nil {
		return fmt.Errorf("adding receipt to store: %w", err)
	}
	if batchRotated {
		if err := s.enqueueEgressTrackTask(ctx, rotatedBatchCID); err != nil {
			return fmt.Errorf("enqueuing egress track task: %w", err)
		}
	}
	return nil
}

func (s *Service) enqueueEgressTrackTask(ctx context.Context, batchCID cid.Cid) error {
	return s.queue.Enqueue(ctx, batchCID)
}

func (s *Service) egressTrack(ctx context.Context, batchCID cid.Cid) error {
	// No service connection configured: journaling/cleanup still run, but the
	// outbound /space/egress/track invocation is short-circuited.
	if s.egressTrackerConn == nil {
		return ErrNotMigrated
	}
	endpoint := capabilities.CborURL(*s.batchEndpoint)
	trackInv, err := egress.Track.Invoke(
		s.id,
		s.egressTrackerDID,
		&egress.TrackArguments{
			Receipts: batchCID,
			Endpoint: endpoint,
		},
		invocation.WithAudience(s.egressTrackerDID),
		invocation.WithProofs(s.egressTrackerProofs...),
		invocation.WithNoExpiration(),
	)
	if err != nil {
		return fmt.Errorf("creating invocation: %w", err)
	}

	resp, err := s.egressTrackerConn.Execute(execution.NewRequest(ctx, trackInv))
	if err != nil {
		return fmt.Errorf("executing invocation: %w", err)
	}

	rcpt := resp.Receipt()
	if rcpt == nil {
		return fmt.Errorf("response missing receipt for invocation: %s", trackInv.Link())
	}
	if rcpt.Out().IsErr() {
		// TODO(forrest)[ucan1]: this error message isn't helpful with just bytes, might need a ucan error schema.
		_, errBytes := rcpt.Out().Unpack()
		return fmt.Errorf("invocation failed: %s", string(errBytes))
	}

	// The egress tracker forks a `/space/egress/consolidate` invocation as a
	// side effect. In UCAN 1.0 side-effect invocations travel in the response
	// container metadata rather than on the receipt itself.
	meta := resp.Metadata()
	if meta == nil {
		return fmt.Errorf("response has no metadata container")
	}
	sideEffects := meta.Invocations()
	if len(sideEffects) != 1 {
		return fmt.Errorf("expected exactly one side-effect invocation, got: %d", len(sideEffects))
	}
	consolidateLink := sideEffects[0].Link()

	c := consolidation.New(trackInv, consolidateLink)
	if err := s.consolidationStore.Put(ctx, batchCID, c); err != nil {
		return fmt.Errorf("storing track invocation in consolidation store: %w", err)
	}
	log.Infof("stored track invocation with consolidate invocation %s for batch %s", consolidateLink, batchCID)

	return nil
}

// Start starts the periodic cleanup task that checks for consolidated batches
// and removes them from the store as well as starting the journal rotator if
// enabled.
func (s *Service) Start(ctx context.Context) error {
	if s.cleanupCheckInterval <= 0 {
		log.Info("cleanup task disabled (interval is 0)")
		close(s.cleanupDone)
		return nil
	}

	cleanupCtx, cancel := context.WithCancel(ctx)
	s.cleanupCancel = cancel

	go s.runCleanupTask(cleanupCtx)

	log.Infof("cleanup task started with interval: %v", s.cleanupCheckInterval)

	if s.journalRotator != nil {
		s.journalRotator.Start()
		log.Info("periodic journal rotator started")
	}
	return nil
}

// Stop stops the periodic cleanup task and journal rotator gracefully.
func (s *Service) Stop(ctx context.Context) error {
	if s.journalRotator != nil {
		s.journalRotator.Stop(ctx)
		log.Info("periodic journal rotator stopped")
	}

	if s.cleanupCancel != nil {
		s.cleanupCancel()
	}

	select {
	case <-s.cleanupDone:
		log.Info("cleanup task stopped")
		return nil
	case <-ctx.Done():
		return fmt.Errorf("timeout waiting for cleanup task to stop: %w", ctx.Err())
	}
}

func (s *Service) runCleanupTask(ctx context.Context) {
	defer close(s.cleanupDone)

	ticker := time.NewTicker(s.cleanupCheckInterval)

	for {
		select {
		case <-ctx.Done():
			log.Info("cleanup task context cancelled")
			return
		case <-ticker.C:
			if err := s.cleanupConsolidatedBatches(ctx); err != nil {
				log.Errorf("error cleaning up consolidated batches: %v", err)
			}
		}
	}
}

func (s *Service) cleanupConsolidatedBatches(ctx context.Context) error {
	// List all batches
	batchCIDs, err := s.journal.List(ctx)
	if err != nil {
		return fmt.Errorf("listing batches: %w", err)
	}

	// Check each batch for consolidation
	// TODO: consider doing this in parallel
	for batchCID := range batchCIDs {
		if err := s.checkAndRemoveConsolidatedBatch(ctx, batchCID); err != nil {
			log.Errorf("error checking batch %s: %v", batchCID, err)
			// Continue with other batches even if one fails
		}
	}

	return nil
}

func (s *Service) checkAndRemoveConsolidatedBatch(ctx context.Context, batchCID cid.Cid) error {
	// Get the consolidation data from the consolidation store
	c, err := s.consolidationStore.Get(ctx, batchCID)
	if err != nil {
		log.Warnf("batch %s not found in consolidation store, skipping: %v", batchCID, err)
		return nil
	}

	rcpt, err := s.rcptsClient.Fetch(ctx, c.ConsolidateInvocationCID)
	if err != nil {
		if errors.Is(err, receipts.ErrNotFound) {
			log.Debugf("consolidate receipt not yet available for batch %s", batchCID)
			return nil
		}

		return fmt.Errorf("fetching consolidate receipt: %w", err)
	}

	if err := s.validateConsolidateReceipt(rcpt); err != nil {
		return fmt.Errorf("receipt failed validation: %w", err)
	}

	// Remove the batch from the store
	log.Infof("consolidate receipt found for batch %s, removing from store", batchCID)
	if err := s.journal.Remove(ctx, batchCID); err != nil {
		return fmt.Errorf("removing consolidated batch: %w", err)
	}

	// Remove from consolidation store
	if err := s.consolidationStore.Delete(ctx, batchCID); err != nil {
		log.Warnf("failed to remove batch %s from consolidation store: %v", batchCID, err)
	}

	log.Debugf("batch %s removed from journal and consolidation store", batchCID.String())

	return nil
}

func (s *Service) validateConsolidateReceipt(_ ucan.Receipt) error {
	// TODO: Validate the receipt. This will include checking the receipt matches the original track invocation
	// and confirming that the consolidated amount of bytes matches our records.
	return nil
}
