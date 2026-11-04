-- NB(piri): the store behind Curio's pdpv0.ProofCacheStore. Curio keeps these
-- layers in its Cassandra index store, one row per node; Piri runs no index
-- store, and a whole layer is at most 8192 nodes (256 KiB), so it keeps one
-- row per layer here.
--
-- Prove reads a sub-piece above 32 MiB padded only through this cache, and
-- pdpv0's SaveCache task fills it for every pdp_piecerefs row with
-- needs_save_cache set.
CREATE TABLE pdp_proof_cache_layers (
    -- Curio keys the cache by the v2 piece CID, the form Piri parks in
    -- pdp_piecerefs.piece_cid, so the layer can be dropped with the piece's
    -- last ref.
    piece_cid text NOT NULL,
    layer_index integer NOT NULL,
    -- The layer's 32-byte nodes in index order: node i is at byte 32*i.
    nodes bytea NOT NULL,
    PRIMARY KEY (piece_cid, layer_index)
);

-- Piri's piece removal and Curio's piece GC both delete pdp_piecerefs rows;
-- a piece's layers go once no ref to it is left.
CREATE OR REPLACE FUNCTION pdp_proof_cache_layers_drop_unreferenced() RETURNS trigger AS $$
BEGIN
    DELETE FROM pdp_proof_cache_layers c
    WHERE c.piece_cid = OLD.piece_cid
      AND NOT EXISTS (SELECT 1 FROM pdp_piecerefs pr WHERE pr.piece_cid = OLD.piece_cid);
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER pdp_proof_cache_layers_drop_unreferenced
    AFTER DELETE ON pdp_piecerefs
    FOR EACH ROW EXECUTE FUNCTION pdp_proof_cache_layers_drop_unreferenced();

-- Curio's indexes for pdpv0's save-cache task (its
-- 20260624-db-scale-indexes.sql). Its scheduler claims the oldest unclaimed
-- piece every few seconds, and Piri's reclaim looks pieces up by task id.
CREATE INDEX IF NOT EXISTS idx_pdp_piecerefs_save_cache_pending
    ON pdp_piecerefs (created_at) WHERE save_cache_task_id IS NULL AND needs_save_cache = TRUE;

CREATE INDEX IF NOT EXISTS idx_pdp_piecerefs_save_cache_task
    ON pdp_piecerefs (save_cache_task_id) WHERE save_cache_task_id IS NOT NULL;

-- NB(piri): not Curio's. piri_pdp_proof_cache_pending_oldest_seconds reads
-- the oldest piece still waiting for its layer, claimed by a task or not, and
-- stops at the first one; Curio's pending index holds only unclaimed pieces,
-- which its scheduler claims within seconds.
CREATE INDEX IF NOT EXISTS idx_pdp_piecerefs_needs_save_cache
    ON pdp_piecerefs (created_at) WHERE needs_save_cache = TRUE;
