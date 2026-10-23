-- NB(piri): the signed head of this node's IPNI advertisement chain, one row,
-- moved by compare-and-swap in the transaction that publishes a batch. The
-- adverts it links to live in the blob store (ipni/<cid> in the bucket, or
-- data_dir/ipni on a local volume) and are written before the head moves.
CREATE TABLE ipni_head (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    head bytea NOT NULL -- dag-json SignedHead
);

-- NB(piri): go-ipni-tools' maps from context ID to the entries chunk and to
-- the metadata last advertised under it, for this node's own peer ID. Each
-- node has its own database, so the provider is not stored.
CREATE TABLE ipni_contexts (
    context_id bytea PRIMARY KEY,
    chunk_link bytea, -- binary CID of the entries chunk
    metadata bytea    -- go-ipni-tools metadata bytes
);
