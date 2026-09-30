-- NB(piri): the upload's bytes stay under the upload's key once their digest is
-- known, until the commP task reads them and settles them at the key of their
-- digest in the same pass. Until then this maps the digest to the upload, and
-- reads go through it. One upload holds a digest: a second upload of the same
-- content drops its own bytes.
CREATE TABLE pdp_blob_uploads (
    digest     bytea PRIMARY KEY,
    upload_id  text NOT NULL UNIQUE,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP
);
