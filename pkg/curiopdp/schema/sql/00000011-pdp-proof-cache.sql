-- NB(piri): the store behind Curio's pdpv0.ProofCacheStore. Curio keeps these
-- layers in its Cassandra index store, one row per node; Piri runs no index
-- store, and a whole layer is at most 8192 nodes (256 KiB), so it keeps one
-- row per layer here.
--
-- Prove reads a sub-piece above 32 MiB padded only through this cache, and
-- pdpv0's SaveCache task fills it for every pdp_piecerefs row with
-- needs_save_cache set.
CREATE TABLE pdp_proof_cache_layers (
    -- Curio keys the cache by the sized (v2) piece CID.
    piece_cid_v2 bytea NOT NULL,
    -- The same piece as pdp_piecerefs.piece_cid holds it (v1), so the layer
    -- can be dropped with the piece's last ref.
    piece_cid text NOT NULL,
    layer_index integer NOT NULL,
    -- The layer's 32-byte nodes in index order: node i is at byte 32*i.
    nodes bytea NOT NULL,
    PRIMARY KEY (piece_cid_v2, layer_index)
);

CREATE INDEX pdp_proof_cache_layers_piece_cid_idx ON pdp_proof_cache_layers (piece_cid);

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
