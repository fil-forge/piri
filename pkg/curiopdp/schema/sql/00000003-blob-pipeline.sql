-- NB(piri): harmonytask-native aggregation pipeline state (replaces the
-- commp/aggregator/manager jobqueues that ran on the separate piri-local
-- aggregator_db). One pdp_blob_pipeline row per accepted blob, from
-- /blob/accept until its aggregate's pieces are staged on-chain; the removal
-- machinery reads it transactionally to answer "is this digest still in
-- flight?".
--
-- Column typing follows the curio tables this pipeline joins against
-- (pdp_piece_mh_to_commp: mhash bytea, commp text): multihash digests are
-- raw bytes, piece CIDs are text strings.

CREATE TABLE pdp_blob_pipeline (
    digest bytea NOT NULL, -- blob multihash, raw bytes
    -- stage 1: commp. The task id is provenance for the aggregation task's
    -- Follows crash net and is never cleared; commp is the v2 piece CID
    -- string, set when the stage completes.
    commp_task_id bigint,
    commp text,
    commp_hashed_at timestamptz, -- stamped with commp; per-stage age for stuck-row triage
    -- stage 2: aggregation. agg_task_id records the per-piece aggregation
    -- task spawned for this blob (spawn dedup); the fold itself operates on
    -- every unaggregated row under the task's Max=1 serialization.
    agg_task_id bigint,
    aggregate_root text, -- aggregate root piece CID (v2), set when folded in
    aggregated_at timestamptz, -- stamped with aggregate_root
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT pdp_blob_pipeline_pk PRIMARY KEY (digest)
);
CREATE INDEX pdp_blob_pipeline_unaggregated_idx
    ON pdp_blob_pipeline (created_at) WHERE aggregate_root IS NULL;

-- stage 3: root submission buffer (the manager). One row per aggregate
-- root awaiting an addPieces submission; rows are claimed by a PDPAddRoots
-- batch task and deleted (with their pipeline rows) once the batch is
-- submitted on-chain.
CREATE TABLE pdp_root_submissions (
    root text NOT NULL, -- aggregate root piece CID (v2)
    add_task_id bigint,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT pdp_root_submissions_pk PRIMARY KEY (root)
);
