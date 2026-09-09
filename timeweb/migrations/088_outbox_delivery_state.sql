-- Outbox rows are durable records of attempted customer communication. When
-- SMTP is intentionally disabled by configuration, a letter is not pending:
-- the mail integration contract says that no letter will be sent. Preserve
-- that fact explicitly instead of leaving an unsendable row in the worker
-- queue forever or pretending that it was sent.
ALTER TABLE outbox
  ADD COLUMN IF NOT EXISTS cancelled_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS cancel_reason TEXT NOT NULL DEFAULT '';

DROP INDEX IF EXISTS outbox_pending_idx;
CREATE INDEX IF NOT EXISTS outbox_pending_idx ON outbox (id)
  WHERE sent_at IS NULL AND cancelled_at IS NULL;
