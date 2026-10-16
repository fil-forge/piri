-- NB(piri): the per-blob records the UCAN handlers keep, which used to live
-- in one object-store bucket (or LevelDB) per store. Digests are raw
-- multihash bytes and CIDs are text strings, as in the pdp_* tables. Signed
-- UCAN envelopes are kept as the bytes they were received or issued as.

-- A space's claim on a blob: the allocation /blob/allocate made, or the
-- received upload a digest-less allocation handed it to. allocation names
-- the /blob/allocate task, and a /blob/reject finds the record by it.
CREATE TABLE ucan_allocations (
    digest bytea NOT NULL,
    space text NOT NULL,
    size bigint NOT NULL,
    expires bigint NOT NULL, -- unix seconds
    cause text NOT NULL,
    allocation text,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ucan_allocations_pk PRIMARY KEY (digest, space)
);

CREATE UNIQUE INDEX ucan_allocations_allocation ON ucan_allocations (allocation)
    WHERE allocation IS NOT NULL;

-- An allocation made without a digest, keyed by its /blob/allocate task.
-- digest is NULL until the upload has received and hashed the data.
CREATE TABLE ucan_pending_allocations (
    allocation text NOT NULL,
    space text NOT NULL,
    size bigint NOT NULL,
    digest_code bigint NOT NULL,
    cause text NOT NULL,
    expires bigint NOT NULL, -- unix seconds
    upload_id text NOT NULL,
    digest bytea,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ucan_pending_allocations_pk PRIMARY KEY (allocation)
);

-- Releasing a claim hands it to another received upload of the same content
-- in the same space.
CREATE INDEX ucan_pending_allocations_digest ON ucan_pending_allocations (digest, space)
    WHERE digest IS NOT NULL;

-- A space's acceptance of a blob. pdp_accept is the /pdp/accept task its
-- promise awaits; site is the location commitment's CID.
CREATE TABLE ucan_acceptances (
    digest bytea NOT NULL,
    space text NOT NULL,
    size bigint NOT NULL,
    pdp_accept text NOT NULL,
    executed_at bigint NOT NULL, -- unix seconds
    cause text NOT NULL,
    site text NOT NULL,
    allocation text,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ucan_acceptances_pk PRIMARY KEY (digest, space)
);

-- Invocations the node issued and serves, the location commitments.
CREATE TABLE ucan_invocations (
    cid text NOT NULL,
    bytes bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ucan_invocations_pk PRIMARY KEY (cid)
);

-- Receipts, found by their own CID or by the task they ran. The latest
-- receipt stored for a task is the one found by it.
CREATE TABLE ucan_receipts (
    cid text NOT NULL,
    ran text NOT NULL,
    bytes bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ucan_receipts_pk PRIMARY KEY (cid)
);

CREATE UNIQUE INDEX ucan_receipts_ran ON ucan_receipts (ran);

-- Egress batches: the track invocation and the consolidate invocation that
-- settles it, keyed by batch CID.
CREATE TABLE ucan_consolidations (
    batch text NOT NULL,
    track_invocation bytea NOT NULL,
    consolidate_invocation text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ucan_consolidations_pk PRIMARY KEY (batch)
);
