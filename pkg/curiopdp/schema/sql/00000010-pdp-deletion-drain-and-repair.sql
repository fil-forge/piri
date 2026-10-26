-- NB(piri): brings the closure from Curio 38ca280c43a2 up to 127dea11. These
-- are Curio's harmonydb migrations from that range, each under the name of its
-- Curio file, keeping only the statements on tables the closure holds or that
-- Curio's code at 127dea11 reads. Statements on Curio subsystems Piri does
-- not run (sealing, IPNI, market, Filecoin messages) are left out.

-- 20260424-seal-poller-indexes.sql
CREATE INDEX IF NOT EXISTS idx_parked_pieces_pending
    ON parked_pieces (long_term)
    WHERE complete = FALSE AND task_id IS NULL AND skip = FALSE;

-- 20260715-message-send-replacements.sql
CREATE TABLE IF NOT EXISTS message_send_eth_replacements (
    id bigserial PRIMARY KEY,
    from_address text NOT NULL,
    nonce bigint NOT NULL,
    original_signed_hash text NOT NULL,
    replaces_signed_hash text NOT NULL,
    claim_id text NOT NULL,
    claim_time timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    claim_until timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP + interval '5 minutes',
    signed_hash text,
    signed_tx bytea,
    send_time timestamptz,
    send_success boolean,
    send_error text
);

CREATE INDEX IF NOT EXISTS message_send_eth_replacements_latest_success_idx
    ON message_send_eth_replacements (from_address, nonce, signed_hash)
    WHERE send_success = TRUE
      AND signed_hash IS NOT NULL;

CREATE INDEX IF NOT EXISTS message_send_eth_replacements_next_success_idx
    ON message_send_eth_replacements (from_address, nonce, replaces_signed_hash)
    WHERE send_success = TRUE;

CREATE UNIQUE INDEX IF NOT EXISTS message_send_eth_replacements_claim_idx
    ON message_send_eth_replacements (from_address, nonce, replaces_signed_hash)
    WHERE send_success IS NOT FALSE;

CREATE INDEX IF NOT EXISTS message_send_eth_replacements_signed_hash_idx
    ON message_send_eth_replacements (signed_hash)
    WHERE signed_hash IS NOT NULL;

CREATE INDEX IF NOT EXISTS message_send_eth_replacements_cleanup_done_idx
    ON message_send_eth_replacements (from_address, nonce)
    WHERE send_success IS NOT NULL;

CREATE INDEX IF NOT EXISTS message_send_eth_replacements_cleanup_claim_idx
    ON message_send_eth_replacements (from_address, nonce, claim_until)
    WHERE send_success IS NULL;

CREATE INDEX IF NOT EXISTS message_send_eth_replacements_original_success_idx
    ON message_send_eth_replacements (original_signed_hash, signed_hash)
    WHERE send_success = TRUE
      AND signed_hash IS NOT NULL;

CREATE INDEX IF NOT EXISTS message_send_eth_replacements_original_next_idx
    ON message_send_eth_replacements (original_signed_hash, replaces_signed_hash)
    WHERE send_success = TRUE;

-- 20260717-pdp-v0-drop-prove-backoff.sql
ALTER TABLE pdp_data_sets DROP COLUMN IF EXISTS consecutive_prove_failures;
ALTER TABLE pdp_data_sets DROP COLUMN IF EXISTS next_prove_attempt_at;

ALTER TABLE pdp_data_sets
    ADD COLUMN IF NOT EXISTS pp_reconcile_needed BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_pdp_data_sets_pp_reconcile_needed
    ON pdp_data_sets (id)
    WHERE pp_reconcile_needed = TRUE;

-- 20260729-message-sends-eth-timestamptz.sql
ALTER TABLE message_sends_eth
ALTER COLUMN send_time TYPE TIMESTAMPTZ
    USING send_time AT TIME ZONE current_setting('TimeZone');

ALTER TABLE message_send_eth_locks
ALTER COLUMN claimed_at TYPE TIMESTAMPTZ
    USING claimed_at AT TIME ZONE current_setting('TimeZone');

-- 20260730-pdp-v0-create-extra-data.sql
ALTER TABLE pdp_data_set_creates ADD COLUMN IF NOT EXISTS extra_data BYTEA;

-- 20260730-pdp-v0-dataset-ipfs-indexing.sql
ALTER TABLE pdp_data_sets ADD COLUMN IF NOT EXISTS ipfs_indexing BOOLEAN;

COMMENT ON COLUMN pdp_data_sets.ipfs_indexing IS
    'withIPFSIndexing intent: TRUE/FALSE once known, NULL until resolved from create extra_data or chain';

-- 20260804-pdpv0-repair-missing-pieces.sql
CREATE TABLE IF NOT EXISTS pdp_data_set_piece_scrub (
    data_set BIGINT PRIMARY KEY REFERENCES pdp_data_sets(id) ON DELETE CASCADE,
    next_piece_id BIGINT NOT NULL DEFAULT 0,
    task_id BIGINT DEFAULT NULL,
    complete BOOLEAN NOT NULL DEFAULT FALSE,
    failures BIGINT NOT NULL DEFAULT 0,
    pieces_checked BIGINT NOT NULL DEFAULT 0,
    pieces_repaired BIGINT NOT NULL DEFAULT 0,
    pieces_lost BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT TIMEZONE('UTC', NOW()),
    completed_at TIMESTAMPTZ DEFAULT NULL
);

COMMENT ON TABLE pdp_data_set_piece_scrub IS
    'One-shot queue of data sets to reconcile against PDPVerifier active pieces; seeded at upgrade time only.';

CREATE INDEX IF NOT EXISTS idx_pdp_data_set_piece_scrub_pending
    ON pdp_data_set_piece_scrub (data_set)
    WHERE complete = FALSE AND task_id IS NULL;

CREATE INDEX IF NOT EXISTS idx_pdp_data_set_piece_scrub_task_id
    ON pdp_data_set_piece_scrub (task_id)
    WHERE task_id IS NOT NULL;

-- NB(piri): Piri does not register Curio's repair task yet, so these rows
-- wait for it; seeding them now keeps the datasets that predate the receipt
-- fix in the queue for when it is registered.
INSERT INTO pdp_data_set_piece_scrub (data_set)
SELECT id FROM pdp_data_sets
ON CONFLICT (data_set) DO NOTHING;

CREATE TABLE IF NOT EXISTS lost_data (
    data_set BIGINT NOT NULL,
    piece_id BIGINT NOT NULL,
    piece_cid TEXT NOT NULL,
    piece_cid_v1 TEXT DEFAULT NULL,
    piece_raw_size BIGINT DEFAULT NULL,
    reason TEXT NOT NULL,
    detected_at TIMESTAMPTZ NOT NULL DEFAULT TIMEZONE('UTC', NOW()),
    PRIMARY KEY (data_set, piece_id)
);

COMMENT ON TABLE lost_data IS
    'Pieces live on PDPVerifier whose data could not be located in local storage during a repair scrub.';

-- 20260807-singleton-task-disabled.sql
CREATE TABLE IF NOT EXISTS harmony_task_singleton_disabled (
    task_name varchar(255) NOT NULL PRIMARY KEY,
    disabled_at timestamptz NOT NULL DEFAULT current_timestamp,
    reason text
);

-- 20260811-message-send-lock-task-fks.sql
DELETE FROM message_send_eth_locks msel
WHERE NOT EXISTS (
    SELECT 1 FROM harmony_task ht WHERE ht.id = msel.task_id
);

CREATE INDEX IF NOT EXISTS message_send_eth_locks_task_id_idx
    ON message_send_eth_locks (task_id);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'message_send_eth_locks_task_id_fkey'
          AND conrelid = 'message_send_eth_locks'::regclass
    ) THEN
        ALTER TABLE message_send_eth_locks
            ADD CONSTRAINT message_send_eth_locks_task_id_fkey
            FOREIGN KEY (task_id) REFERENCES harmony_task (id) ON DELETE CASCADE;
    END IF;
END $$;

-- 20260812-table-analyze-state.sql
CREATE TABLE IF NOT EXISTS table_analyze_state (
    table_name       TEXT PRIMARY KEY,
    churn_at_analyze BIGINT      NOT NULL,
    rows_at_analyze  BIGINT      NOT NULL DEFAULT 0,
    last_analyzed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    analyze_count    BIGINT      NOT NULL DEFAULT 0
);

-- 20260818-pdpv0-deletion-drain.sql
CREATE TABLE IF NOT EXISTS pdpv0_deletion_drain (
    data_set BIGINT PRIMARY KEY REFERENCES pdp_data_sets(id) ON DELETE CASCADE,
    task_id BIGINT REFERENCES harmony_task(id) ON DELETE SET NULL,
    msg_hash TEXT DEFAULT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE pdpv0_deletion_drain IS
    'Data sets that may have a non-empty PDPVerifier scheduled-removal queue to drain via processPieceDeletions.';

CREATE INDEX IF NOT EXISTS idx_pdpv0_deletion_drain_pending
    ON pdpv0_deletion_drain (data_set)
    WHERE task_id IS NULL AND msg_hash IS NULL;

CREATE INDEX IF NOT EXISTS idx_pdpv0_deletion_drain_msg_hash
    ON pdpv0_deletion_drain (msg_hash)
    WHERE msg_hash IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_pdpv0_deletion_drain_task_id
    ON pdpv0_deletion_drain (task_id)
    WHERE task_id IS NOT NULL;

INSERT INTO pdpv0_deletion_drain (data_set)
SELECT id FROM pdp_data_sets
ON CONFLICT (data_set) DO NOTHING;

-- 20260820-pdp-streaming-timestamptz.sql
-- NB(piri): the closure already has both columns as TIMESTAMPTZ, so only the
-- default changes.
ALTER TABLE pdp_piece_streaming_uploads
    ALTER COLUMN created_at SET DEFAULT NOW();

-- 20260828-pdp-v0-pull-items-indexes.sql
CREATE INDEX IF NOT EXISTS idx_pdp_piece_pull_items_incomplete
    ON pdp_piece_pull_items (created_at ASC)
    WHERE complete = FALSE;

CREATE INDEX IF NOT EXISTS idx_pdp_piece_pull_items_task_id
    ON pdp_piece_pull_items (task_id);

-- 20260908-filecoin-payment-retry.sql
ALTER TABLE filecoin_payment_transactions ADD COLUMN IF NOT EXISTS retry BOOLEAN NOT NULL DEFAULT FALSE;

-- 20260923-pdp-v0-drop-cache-timestamps.sql
ALTER TABLE pdp_piecerefs DROP COLUMN IF EXISTS caching_task_started;
ALTER TABLE pdp_piecerefs DROP COLUMN IF EXISTS caching_task_completed;

-- 20260925-pdp-v0-pull-source-order.sql
ALTER TABLE pdp_piece_pull_items
    ADD COLUMN IF NOT EXISTS source_ord INTEGER NOT NULL DEFAULT 0;

COMMENT ON COLUMN pdp_piece_pull_items.source_ord IS
    'Order in which ingest should try this URL for the piece. Lower values are first.';

-- 20261019-pdp-v0-data-sets-challenge-task-index.sql
CREATE INDEX IF NOT EXISTS idx_pdp_data_sets_challenge_request_task_id
    ON pdp_data_sets (challenge_request_task_id);
