package pipeline

import (
	"testing"

	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/stretchr/testify/require"
)

// harmonytask.New refuses to start an engine with a task name longer than 16
// characters, which takes the whole node down at boot.
func TestTaskNamesFitTheEngine(t *testing.T) {
	for _, task := range []harmonytask.TaskInterface{
		&CommPTask{},
		&AggregateTask{},
		&AddRootsTask{},
		&RemoveSweepTask{},
		&ExpireAllocationsTask{},
	} {
		name := task.TypeDetails().Name
		require.LessOrEqual(t, len(name), 16, "task name %q", name)
	}
}
