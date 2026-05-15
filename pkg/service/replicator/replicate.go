package replicator

import (
	"context"

	"github.com/fil-forge/go-ucanto/client"
	"github.com/fil-forge/go-ucanto/principal"

	"github.com/fil-forge/piri/lib/jobqueue"
	"github.com/fil-forge/piri/pkg/pdp"
	"github.com/fil-forge/piri/pkg/service/blobs"
	"github.com/fil-forge/piri/pkg/service/claims"
	replicahandler "github.com/fil-forge/piri/pkg/service/storage/handlers/replica"
	"github.com/fil-forge/piri/pkg/store/receiptstore"
)

type Replicator interface {
	Replicate(context.Context, *replicahandler.TransferRequest) error
}

type Service struct {
	queue   *jobqueue.JobQueue[*replicahandler.TransferRequest]
	deps    replicahandler.TransferDeps
	metrics *replicahandler.Metrics
}

func New(
	id principal.Signer,
	p pdp.PDP,
	b blobs.Blobs,
	c claims.Claims,
	rstore receiptstore.ReceiptStore,
	uploadConn client.Connection,
	queue *jobqueue.JobQueue[*replicahandler.TransferRequest],
) (*Service, error) {
	metrics, err := replicahandler.NewMetrics()
	if err != nil {
		return nil, err
	}
	return &Service{
		queue: queue,
		deps: replicahandler.TransferDeps{
			ID:          id,
			Acceptances: b.Acceptances(),
			Pieces:      p.API(),
			Commp:       p.CommpCalculate(),
			Claims:      c,
			Receipts:    rstore,
			UploadConn:  uploadConn,
		},
		metrics: metrics,
	}, nil
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
