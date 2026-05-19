package replicator

// TODO(forrest)[ucan1]: will do this later maybe
/*
import (
	"github.com/fil-forge/piri/lib/jobqueue"
	replicahandler "github.com/fil-forge/piri/pkg/service/storage/handlers/replica"
)

import (
	"context"

	"github.com/fil-forge/piri/lib/jobqueue"
	replicahandler "github.com/fil-forge/piri/pkg/service/storage/handlers/replica"
)

type Replicator interface {
	Replicate(context.Context, *replicahandler.TransferRequest) error
}

type Service struct {
	queue   *jobqueue.JobQueue[*replicahandler.TransferRequest]
	deps    replicahandler.TransferDeps
	metrics *replicahandler.Metrics
}

const TransferTaskName = "transfer-task"

func (r *Service) Replicate(ctx context.Context, task *replicahandler.TransferRequest) error {
	return r.queue.Enqueue(ctx, TransferTaskName, task)
}

func (r *Service) RegisterTransferTask(queue *jobqueue.JobQueue[*replicahandler.TransferRequest]) error {
	return queue.Register(TransferTaskName, func(ctx context.Context, request *replicahandler.TransferRequest) error {
		return replicahandler.Transfer(ctx, r.deps, request, r.metrics)
	}, jobqueue.WithOnFailure(func(ctx context.Context, msg *replicahandler.TransferRequest, err error) error {
		return replicahandler.SendFailureReceipt(ctx, r.deps, msg, err)
	}))
}

*/
