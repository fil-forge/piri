-- NB(piri): an upload whose allocation named only the hash function has no
-- digest to check the data against: the digest is computed as the data is
-- received, and the data is written under a key named by the upload.
-- check_hash is NULL for such an upload, and allocation links the
-- `/blob/allocate` task whose pending allocation records the computed digest.
ALTER TABLE pdp_piece_uploads ALTER COLUMN check_hash DROP NOT NULL;
ALTER TABLE pdp_piece_uploads ADD COLUMN allocation text;
