package publisher

import (
	"context"
	"errors"
	"time"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"

	"github.com/fil-forge/piri/pkg/pdp/promise"
)

const PublishTaskName = "IPNIPublish"

const (
	// publishPollInterval bounds how long an accepted blob waits for its
	// advertisement: every interval whatever is queued is claimed and
	// published, a partial batch included.
	publishPollInterval = 5 * time.Second
	// publishBatchSize caps the advertisements published under one commit.
	publishBatchSize = 1000
)

// PublishTask publishes queued IPNI advertisements in batches. Each run
// claims a chunk of ipni_pending_adverts, publishes it under one
// advertisement chain commit (one signed head and one announce, however many
// blobs) and retires the rows. It is what lets /blob/accept return before its
// advertisement exists.
//
// Runs are scheduled by the task's own ticker rather than the engine's
// IAmBored: the engine only asks a task for work when it has none, and the
// PDP pipeline keeps it busy for as long as it takes to commP a large
// upload, which is exactly when advertisements are waiting.
//
// It is a singleton: the advertisement chain is serial, so a second worker
// would only wait on the publisher's mutex.
type PublishTask struct {
	queue *DBQueue
	svc   *PublisherService

	add promise.Promise[harmonytask.AddTaskFunc]
}

func NewPublishTask(queue *DBQueue, svc *PublisherService) *PublishTask {
	return &PublishTask{queue: queue, svc: svc}
}

// run flushes the queue every publishPollInterval until ctx ends. A flush
// claims whatever is unclaimed, a partial batch included; when there is
// nothing to claim no task is created.
func (t *PublishTask) run(ctx context.Context) {
	ticker := time.NewTicker(publishPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.flush(ctx)
		}
	}
}

// flush returns stranded rows to the pool, then asks the engine for a task
// that claims whatever is unclaimed.
func (t *PublishTask) flush(ctx context.Context) {
	// Rows stamped by a task the engine has since deleted (it gives up on a
	// task after MaxFailures) would otherwise never be claimed again.
	if n, err := t.queue.reclaimOrphans(ctx); err != nil {
		log.Warnw("reclaiming orphaned advertisements", "error", err)
	} else if n > 0 {
		log.Warnw("reclaimed advertisements from a publish task the engine gave up on", "claims", n)
	}
	t.schedule(ctx)
}

// schedule asks the engine for a task that claims the unclaimed rows. It
// waits for the engine to have handed over its add function, which happens
// when the engine starts.
func (t *PublishTask) schedule(ctx context.Context) {
	add := t.add.Val(ctx)
	if add == nil {
		return
	}
	add(func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) {
		return claimBatch(tx, id, publishBatchSize)
	})
}

// errLostOwnership is returned when the engine reassigned the task while it
// was running; the new owner publishes the batch.
var errLostOwnership = errors.New("publish task is no longer owned by this worker")

func (t *PublishTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()
	claims, err := t.queue.claimed(ctx, taskID)
	if err != nil {
		return false, err
	}
	if len(claims) == 0 {
		return true, nil
	}
	// A failure leaves the rows stamped for this task, and harmonytask
	// retries it with the same batch. That is safe on the publisher's side
	// too: a failed commit leaves nothing behind, and anything that did
	// publish is skipped as already advertised.
	//
	// The batch is only this worker's to publish while the engine still says
	// so; a worker the task was taken from leaves it to the new owner.
	if !stillOwned() {
		return false, errLostOwnership
	}
	if err := t.svc.PublishClaims(ctx, claims); err != nil {
		return false, err
	}
	log.Infow("published advertisement batch", "claims", len(claims))
	return true, t.queue.retire(ctx, taskID)
}

func (t *PublishTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (t *PublishTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(1),
		Name: PublishTaskName,
		// IO-bound: a batch is store writes and one announce. Costing it a
		// CPU would queue it behind the pipeline's commP tasks for capacity.
		Cost: resources.Resources{
			Cpu: 0,
			Ram: 64 << 20,
		},
		MaxFailures: 50,
		RetryWait:   taskhelp.RetryWaitLinear(10*time.Second, 10*time.Second),
	}
}

func (t *PublishTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	t.add.Set(taskFunc)
}

var _ = harmonytask.Reg(&PublishTask{})
var _ harmonytask.TaskInterface = &PublishTask{}
