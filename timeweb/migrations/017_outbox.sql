-- Letters waiting to be sent.
--
-- Mail goes through a queue rather than straight out of the request: a slow
-- or unreachable SMTP server must never hold up an order. The row is written
-- inside the same work that caused it, so a letter is never lost because the
-- process restarted a moment later.
CREATE TABLE IF NOT EXISTS outbox (
  id BIGSERIAL PRIMARY KEY,
  recipient TEXT NOT NULL,
  subject TEXT NOT NULL,
  body TEXT NOT NULL,
  attempts SMALLINT NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT '',
  sent_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- The worker only ever looks for unsent letters, so that is what the index
-- covers; sent ones stay as a record of what the customer was told.
CREATE INDEX IF NOT EXISTS outbox_pending_idx ON outbox (id)
  WHERE sent_at IS NULL;
