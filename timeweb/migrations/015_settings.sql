-- Settings the owner can change without a redeploy.
--
-- Only things a person legitimately decides live here: whether an
-- integration is on, how long an unpaid order waits, who the sender is.
-- Secrets stay in the environment — a key in a table is a key that leaks
-- through a backup, and we have been burnt by that once already.
CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT '',
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
