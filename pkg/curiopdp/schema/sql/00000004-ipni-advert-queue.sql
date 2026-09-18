-- NB(piri): location commitments whose IPNI advertisement is still owed,
-- written by /blob/accept, which no longer publishes inline. The IPNIPublish
-- task claims a chunk of rows by stamping publish_task_id, publishes the
-- chunk under one advertisement chain commit (one signed head, one announce)
-- and deletes the rows. /blob/release deletes the row of a blob released
-- before its advertisement was published.
CREATE TABLE ipni_pending_adverts (
    claim text NOT NULL, -- location commitment CID; the claim store holds the invocation
    publish_task_id bigint, -- NULL until an IPNIPublish task claims the row
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ipni_pending_adverts_pk PRIMARY KEY (claim)
);

CREATE INDEX ipni_pending_adverts_unclaimed ON ipni_pending_adverts (created_at)
    WHERE publish_task_id IS NULL;

-- A task loads and retires its rows by publish_task_id.
CREATE INDEX ipni_pending_adverts_claimed ON ipni_pending_adverts (publish_task_id, created_at)
    WHERE publish_task_id IS NOT NULL;
