-- NB(piri): what to advertise for a queued location commitment, computed
-- when the row is written so that publishing needs nothing but the row: no
-- claim store read, and the publishing lock is held only for the batch's
-- own work. The spec is CBOR (pkg/service/publisher/advert), so its shape
-- can change without another migration. Rows queued before this column
-- existed have it NULL; the publish task skips those with a warning and
-- retires them with their batch.
ALTER TABLE ipni_pending_adverts ADD COLUMN spec bytea;
