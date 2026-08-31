-- Wildberries is mirrored into PostgreSQL by one restart-safe worker.
-- Screens and procurement flows read this local copy and never start their
-- own catalogue or sales export.
CREATE TABLE IF NOT EXISTS procurement_wb_sync_state (
  resource TEXT PRIMARY KEY CHECK (resource IN ('catalog', 'sales')),
  status TEXT NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending', 'running', 'ok', 'error', 'disabled')),
  last_attempt_at TIMESTAMPTZ,
  last_success_at TIMESTAMPTZ,
  next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  locked_until TIMESTAMPTZ,
  rows_synced INTEGER NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT '',
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO procurement_wb_sync_state (resource)
VALUES ('catalog'), ('sales')
ON CONFLICT (resource) DO NOTHING;

-- A database-backed request gate coordinates deployments and parallel app
-- instances. The old in-memory pacer remains a second line of defence, but it
-- is no longer the only process that knows when WB may be called again.
CREATE TABLE IF NOT EXISTS procurement_wb_rate_limits (
  bucket TEXT PRIMARY KEY,
  next_request_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

ALTER TABLE procurement_channel_products
  ADD COLUMN IF NOT EXISTS barcodes TEXT[] NOT NULL DEFAULT '{}';

