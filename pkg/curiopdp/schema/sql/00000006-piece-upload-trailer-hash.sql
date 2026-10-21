-- NB(piri): an upload whose allocation named only the hash function has no
-- digest to check the data against: the digest is computed as the data is
-- received, and the data is written under a key named by the upload.
-- check_hash is NULL for such an upload, and allocation links the
-- `/blob/allocate` task whose pending allocation records the computed digest.
ALTER TABLE pdp_piece_uploads ALTER COLUMN check_hash DROP NOT NULL;
ALTER TABLE pdp_piece_uploads ADD COLUMN allocation text;

-- NB(piri): a discarded upload keeps its row, marked, until whatever wrote its
-- data is known to have stopped: an upload completing after its discard
-- finds the mark and drops its own data, and the expiry task drops the data
-- and the row of one that never completes, which nothing else would find.
ALTER TABLE pdp_piece_uploads ADD COLUMN discarded_at timestamp with time zone;

-- NB(piri): the upload's bytes stay staged under the upload's key once their
-- digest is known, until the settle task moves them to the key of their digest.
-- Until then this maps the digest to the staged upload, and reads go through
-- it. One staged upload holds a digest: a
-- second upload of the same content drops its own bytes.
CREATE TABLE pdp_staged_blobs (
    digest     bytea PRIMARY KEY,
    upload_id  text NOT NULL UNIQUE,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP
);

-- NB(piri): an accepted blob that is still staged is settled at the key of its
-- digest before its commP is calculated. staged marks such a row from enqueue
-- until its settle task has moved the blob; settle_task_id claims the row for
-- that task, as commp_task_id does for the commP task. A row that was never
-- staged starts at commP, as before.
ALTER TABLE pdp_blob_pipeline ADD COLUMN settle_task_id bigint;
ALTER TABLE pdp_blob_pipeline ADD COLUMN staged boolean NOT NULL DEFAULT false;
CREATE INDEX pdp_blob_pipeline_staged_idx
    ON pdp_blob_pipeline (created_at) WHERE staged AND settle_task_id IS NULL;
