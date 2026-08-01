package pipeline

import (
	"context"
	"time"

	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
)

const RemoveSweepTaskName = "PDPRemoveSweep"

// removeSweepInterval bounds how long queued blob removals wait before the
// next sweep pass advances them (parked-blob byte release latency).
const removeSweepInterval = 30 * time.Second

// RemovalSweeper is the slice of the PDP service the sweep task drives; see
// PDPService.ProcessPendingRemovals for the sweep semantics.
type RemovalSweeper interface {
	ProcessPendingRemovals(ctx context.Context) error
}

// RemoveSweepTask periodically advances queued blob removals
// (pdp_pending_piece_removals). It is a harmonytask singleton: IAmBored
// re-arms it every removeSweepInterval, and at most one instance runs at a
// time.
type RemoveSweepTask struct {
	sweeper RemovalSweeper
}

func NewRemoveSweepTask(sweeper RemovalSweeper) *RemoveSweepTask {
	return &RemoveSweepTask{sweeper: sweeper}
}

func (t *RemoveSweepTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	if !stillOwned() {
		return false, nil
	}
	if err := t.sweeper.ProcessPendingRemovals(context.Background()); err != nil {
		// Surface partial failures through task history; the singleton
		// re-arms on the next interval regardless.
		return false, err
	}
	return true, nil
}

func (t *RemoveSweepTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (t *RemoveSweepTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(1),
		Name: RemoveSweepTaskName,
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 64 << 20,
		},
		MaxFailures: 3,
		RetryWait:   taskhelp.RetryWaitLinear(5*time.Second, 5*time.Second),
		IAmBored:    harmonytask.SingletonTaskAdder(removeSweepInterval, t),
	}
}

func (t *RemoveSweepTask) Adder(taskFunc harmonytask.AddTaskFunc) {}

var _ = harmonytask.Reg(&RemoveSweepTask{})
var _ harmonytask.TaskInterface = &RemoveSweepTask{}
