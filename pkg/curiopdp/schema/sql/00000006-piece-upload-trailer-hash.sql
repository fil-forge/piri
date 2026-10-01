-- NB(piri): an upload whose allocation named only the hash function has no
-- digest to check the data against: the digest is computed as the data is
-- received, and the data is written under a key named by the upload.
-- check_hash is NULL for such an upload, and allocation links the
-- `/blob/allocate` task whose pending allocation records the computed digest.
ALTER TABLE pdp_piece_uploads ALTER COLUMN check_hash DROP NOT NULL;
ALTER TABLE pdp_piece_uploads ADD COLUMN allocation text;

-- NB(piri): the upload's bytes stay staged under the upload's key once their
-- digest is known, until the commP task reads them and settles them at the key
-- of their digest in the same pass. Until then this maps the digest to the
-- staged upload, and reads go through it. One staged upload holds a digest: a
-- second upload of the same content drops its own bytes.
CREATE TABLE pdp_staged_blobs (
    digest     bytea PRIMARY KEY,
    upload_id  text NOT NULL UNIQUE,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP
);
