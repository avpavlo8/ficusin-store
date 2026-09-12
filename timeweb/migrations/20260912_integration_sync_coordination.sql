-- Durable coordination for read-only marketplace and Saby imports.
-- One row is one account/method lane. Generations coalesce repeated manual
-- requests while still preserving one follow-up requested during an active run.
CREATE TABLE IF NOT EXISTS procurement_integration_sync_state (
  channel TEXT NOT NULL CHECK (channel IN ('saby', 'wb', 'ozon')),
  resource TEXT NOT NULL CHECK (resource IN ('catalog', 'sales')),
  status TEXT NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending', 'queued', 'running', 'ok', 'error', 'disabled')),
  priority TEXT NOT NULL DEFAULT 'background'
    CHECK (priority IN ('background', 'interactive')),
  requested_generation BIGINT NOT NULL DEFAULT 0,
  active_generation BIGINT NOT NULL DEFAULT 0,
  completed_generation BIGINT NOT NULL DEFAULT 0,
  lease_owner TEXT NOT NULL DEFAULT '',
  lease_token BIGINT NOT NULL DEFAULT 0,
  lease_until TIMESTAMPTZ,
  last_attempt_at TIMESTAMPTZ,
  last_success_at TIMESTAMPTZ,
  next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  next_deep_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  cooldown_until TIMESTAMPTZ,
  period_from DATE,
  period_to DATE,
  latest_event_at TIMESTAMPTZ,
  current_cursor JSONB NOT NULL DEFAULT '{}'::JSONB,
  history_cursor JSONB NOT NULL DEFAULT '{}'::JSONB,
  rows_synced INTEGER NOT NULL DEFAULT 0 CHECK (rows_synced >= 0),
  last_error TEXT NOT NULL DEFAULT '',
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (channel, resource)
);

INSERT INTO procurement_integration_sync_state (
  channel, resource, status, last_attempt_at, last_success_at,
  next_attempt_at, next_deep_at, period_from, period_to, rows_synced, last_error
)
SELECT channel, 'sales', status, last_attempt_at, last_success_at,
  COALESCE(last_success_at + INTERVAL '5 minutes', CURRENT_TIMESTAMP),
  COALESCE(last_success_at + INTERVAL '7 days', CURRENT_TIMESTAMP),
  period_from, period_to, rows_synced, last_error
FROM procurement_sales_sync_state
WHERE channel IN ('saby', 'wb', 'ozon')
ON CONFLICT (channel, resource) DO NOTHING;

INSERT INTO procurement_integration_sync_state (channel, resource, next_attempt_at, next_deep_at)
VALUES
  ('saby', 'catalog', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP + INTERVAL '7 days'),
  ('wb', 'catalog', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP + INTERVAL '7 days'),
  ('ozon', 'catalog', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP + INTERVAL '7 days')
ON CONFLICT (channel, resource) DO NOTHING;

CREATE INDEX IF NOT EXISTS procurement_integration_sync_due_idx
  ON procurement_integration_sync_state (priority DESC, next_attempt_at, channel, resource)
  WHERE status <> 'disabled';

-- A database gate shared by every app instance. The account bucket prevents
-- simultaneous methods for one seller; the method bucket carries its own pace
-- and a server-provided Retry-After cooldown.
CREATE TABLE IF NOT EXISTS procurement_integration_rate_limits (
  channel TEXT NOT NULL CHECK (channel IN ('saby', 'wb', 'ozon')),
  bucket TEXT NOT NULL,
  next_request_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  cooldown_until TIMESTAMPTZ,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (channel, bucket)
);

ALTER TABLE procurement_action_items
  ADD COLUMN IF NOT EXISTS lock_owner TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS lock_token BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS priority TEXT NOT NULL DEFAULT 'background'
    CHECK (priority IN ('background','interactive'));

ALTER TABLE procurement_wb_sync_state
  ADD COLUMN IF NOT EXISTS lease_owner TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS lease_token BIGINT NOT NULL DEFAULT 0;
