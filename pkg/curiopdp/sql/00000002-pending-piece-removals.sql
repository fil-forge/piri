-- NB(piri): blob byte-release requests, written by /blob/remove and
-- /blob/reject when the last space claim on a digest is gone. RemovePiece
-- never deletes inline — the removal machinery advances these rows
-- asynchronously, re-verifying claims (acceptance/allocation stores) and
-- pipeline state before releasing bytes and, for proven pieces, scheduling
-- on-chain deletion (schedulePieceDeletions → rm_message_hash on
-- pdp_data_set_pieces, mirroring Curio's handleDeleteDataSetPiece).
CREATE TABLE pdp_pending_piece_removals (
    blob bytea NOT NULL,
    requested_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT pdp_pending_piece_removals_pk PRIMARY KEY (blob)
);
