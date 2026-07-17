// Package pipeline implements Piri's aggregation pipeline as harmonytask
// tasks on Curio's harmonydb: commP calculation (PDPCommP), aggregate
// folding (PDPAggregate), batched on-chain root submission (PDPAddRoots),
// and the asynchronous blob-removal sweep (PDPRemoveSweep).
//
// All pipeline state lives in harmonydb (pdp_blob_pipeline,
// pdp_root_submissions — see pkg/curiopdp/sql/00000003-blob-pipeline.sql),
// which makes every stage transition transactional with the tables the
// removal machinery inspects. That is the property the previous
// jobqueue-based trio (pkg/pdp/aggregation/{commp,aggregator,manager})
// could not offer: their state was split across a separate aggregator_db
// and an in-memory/datastore buffer, so "is this blob still in flight?"
// was unanswerable without races.
package pipeline

import (
	logging "github.com/ipfs/go-log/v2"
)

var log = logging.Logger("pdp/pipeline")

// taskGroup collects every harmonytask.TaskInterface the curiopdp engine
// runs; it must match the group consumed in pkg/curiopdp/module.go.
const taskGroup = `group:"curio_harmonytasks"`
