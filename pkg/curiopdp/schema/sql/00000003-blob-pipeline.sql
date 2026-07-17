-- NB(piri): harmonytask-native aggregation pipeline state (replaces the
-- commp/aggregator/manager jobqueues that ran on the separate piri-local
-- aggregator_db). One pdp_blob_pipeline row per accepted blob, from
-- /blob/accept until its aggregate's pieces are staged on-chain; the removal
-- machinery reads it transactionally to answer "is this digest still in
-- flight?".

CREATE TABLE pdp_blob_pipeline (
    blob bytea NOT NULL,
    -- stage 1: commp. The task id is provenance for the aggregation task's
    -- Follows crash net and is never cleared; commp is the v2 piece CID
    -- string, set when the stage completes.
    commp_task_id bigint,
    commp text,
    -- stage 2: aggregation. agg_task_id records the per-piece aggregation
    -- task spawned for this blob (spawn dedup); the fold itself operates on
    -- every unaggregated row under the task's Max=1 serialization.
    agg_task_id bigint,
    aggregate_root text, -- aggregate root piece CID (v2), set when folded in
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT pdp_blob_pipeline_pk PRIMARY KEY (blob)
);
CREATE INDEX pdp_blob_pipeline_unaggregated_idx
    ON pdp_blob_pipeline (created_at) WHERE aggregate_root IS NULL;

-- stage 3: root submission buffer (the manager). One row per aggregate
-- root awaiting an addPieces submission; rows are claimed by a PDPAddRoots
-- batch task and deleted (with their pipeline rows) once the batch is
-- submitted on-chain.
CREATE TABLE pdp_root_submissions (
    root text NOT NULL,
    add_task_id bigint,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT pdp_root_submissions_pk PRIMARY KEY (root)
);
