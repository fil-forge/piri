-- filecoin_payment_transactions backs Curio's tasks/pay SettleTask + SettleWatcher
-- (adopted by Piri). It was dropped from the PDP closure (00000000) because the
-- closure-extraction keep-set only matched pdp_* / harmony_* / parked_* /
-- message_*eth* / eth_keys, and this table doesn't match that prefix set.
-- Applied as a follow-on migration (harmonyquery runs it after the closure).
CREATE TABLE IF NOT EXISTS filecoin_payment_transactions (
    tx_hash  TEXT PRIMARY KEY,
    rail_ids BIGINT[] NOT NULL
);
