-- Price approval belongs to one destination at a time. Existing multi-channel
-- batches keep channel='' and their saved actions/payment-independent history.
ALTER TABLE procurement_action_batches
  ADD COLUMN IF NOT EXISTS channel TEXT NOT NULL DEFAULT '';

ALTER TABLE procurement_action_batches
  DROP CONSTRAINT IF EXISTS procurement_action_batches_channel_check;
ALTER TABLE procurement_action_batches
  ADD CONSTRAINT procurement_action_batches_channel_check
  CHECK (channel IN ('', 'site', 'saby_price', 'wb', 'ozon', 'avito'));

DROP INDEX IF EXISTS procurement_action_batches_active_unique;
CREATE UNIQUE INDEX procurement_action_batches_active_unique
  ON procurement_action_batches (procurement_order_id, kind, channel)
  WHERE status NOT IN ('cancelled', 'completed');
