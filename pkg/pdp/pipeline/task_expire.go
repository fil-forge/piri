package pipeline

import (
	"context"
	"time"

	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
)

const ExpireAllocationsTaskName = "PDPAllocExpiry"

// expireAllocationsInterval is how often expired allocations made without a
// digest are reaped. They live for a day, so this only bounds how long past
// expiry their staged data lingers.
const expireAllocationsInterval = 10 * time.Minute

// AllocationExpirer is the slice of the PDP service the expiry task drives;
// see PDPService.ProcessExpiredAllocations.
type AllocationExpirer interface {
	ProcessExpiredAllocations(ctx context.Context) error
}

// ExpireAllocationsTask periodically reaps expired allocations made without a
// digest, with their uploads and staged data. It is a harmonytask singleton:
// IAmBored re-arms it every expireAllocationsInterval, and at most one
// instance runs at a time.
type ExpireAllocationsTask struct {
	expirer AllocationExpirer
}

func NewExpireAllocationsTask(expirer AllocationExpirer) *ExpireAllocationsTask {
	return &ExpireAllocationsTask{expirer: expirer}
}

func (t *ExpireAllocationsTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	if !stillOwned() {
		return false, nil
	}
	if err := t.expirer.ProcessExpiredAllocations(context.Background()); err != nil {
		return false, err
	}
	return true, nil
}

func (t *ExpireAllocationsTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (t *ExpireAllocationsTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(1),
		Name: ExpireAllocationsTaskName,
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 64 << 20,
		},
		MaxFailures: 3,
		RetryWait:   taskhelp.RetryWaitLinear(5*time.Second, 5*time.Second),
		IAmBored:    harmonytask.SingletonTaskAdder(expireAllocationsInterval, t),
	}
}

func (t *ExpireAllocationsTask) Adder(taskFunc harmonytask.AddTaskFunc) {}

var _ = harmonytask.Reg(&ExpireAllocationsTask{})
var _ harmonytask.TaskInterface = &ExpireAllocationsTask{}
