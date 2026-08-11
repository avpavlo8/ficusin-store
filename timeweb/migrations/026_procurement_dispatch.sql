-- Durable marketplace dispatch. A submitted WB operation can outlive a
-- process restart, so its external id and retry lease must be persisted.
ALTER TABLE procurement_action_items
  ADD COLUMN IF NOT EXISTS compare_at_value NUMERIC(14,2),
  ADD COLUMN IF NOT EXISTS external_operation_id TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS last_attempt_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  ADD COLUMN IF NOT EXISTS locked_until TIMESTAMPTZ;

ALTER TABLE procurement_action_items
  DROP CONSTRAINT IF EXISTS procurement_action_items_status_check;
ALTER TABLE procurement_action_items
  ADD CONSTRAINT procurement_action_items_status_check
  CHECK (status IN ('draft', 'approved', 'queued', 'processing', 'completed', 'failed', 'not_configured', 'skipped'));

CREATE INDEX IF NOT EXISTS procurement_action_items_dispatch_idx
  ON procurement_action_items (next_attempt_at, id)
  WHERE status = 'queued';
