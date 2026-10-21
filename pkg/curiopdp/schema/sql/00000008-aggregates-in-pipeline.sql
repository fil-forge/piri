-- NB(piri): an aggregate is the pipeline rows that share its aggregate_root,
-- ordered by agg_position (the piece's index in the aggregate's sub-pieces,
-- the order AddRoots validates). inclusion_proof is the piece's CBOR-encoded
-- merkletree.ProofData. The aggregation task writes both columns in the
-- transaction that sets aggregate_root, and the AddRoots retirement deletes
-- them with the row.
ALTER TABLE pdp_blob_pipeline ADD COLUMN agg_position integer;
ALTER TABLE pdp_blob_pipeline ADD COLUMN inclusion_proof bytea;

-- NB(piri): aggregates folded before this migration live in the local
-- aggregate datastore, not in these columns. Unfold them so the aggregation
-- task folds the blobs again: agg_task_id is cleared too, because the
-- aggregation task's IAmBored only claims rows without one. Their buffered
-- roots go with them. A PDPAddRoots task that had claimed some of them finds
-- no submission rows on its next run and completes without doing anything.
-- A batch that AddRoots landed on chain before the upgrade but did not
-- retire is added a second time.
UPDATE pdp_blob_pipeline
SET aggregate_root = NULL, aggregated_at = NULL, agg_task_id = NULL
WHERE aggregate_root IS NOT NULL;
DELETE FROM pdp_root_submissions;
