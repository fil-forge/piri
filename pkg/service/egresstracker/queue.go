package egresstracker

/*

import (
	"context"

	"github.com/fil-forge/piri/lib/jobqueue"
	"github.com/ipfs/go-cid"
)

type EgressTrackerQueue interface {
	Register(fn func(ctx context.Context, batchCID cid.Cid) error) error
	Enqueue(ctx context.Context, batchCID cid.Cid) error
}

const egressTrackTaskName = "egress-track-task"

var _ EgressTrackerQueue = (*jobQueueAdapter)(nil)

type jobQueueAdapter struct {
	queue *jobqueue.JobQueue[cid.Cid]
}

func NewEgressTrackerQueue(queue *jobqueue.JobQueue[cid.Cid]) EgressTrackerQueue {
	return &jobQueueAdapter{queue: queue}
}

func (a *jobQueueAdapter) Register(fn func(ctx context.Context, batchCID cid.Cid) error) error {
	return a.queue.Register(egressTrackTaskName, fn)
}

func (a *jobQueueAdapter) Enqueue(ctx context.Context, batchCID cid.Cid) error {
	return a.queue.Enqueue(ctx, egressTrackTaskName, batchCID)
}


*/
