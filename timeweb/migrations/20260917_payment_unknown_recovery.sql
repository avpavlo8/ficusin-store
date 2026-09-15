-- A timeout while creating a payment is not a failed payment. Keep the exact
-- request so the same operation can be repeated safely during YooKassa's
-- 24-hour idempotency window. After that window the row remains pending for
-- explicit operator reconciliation; stock must not be released blindly.
ALTER TABLE payments
  ADD COLUMN IF NOT EXISTS request_payload JSONB,
  ADD COLUMN IF NOT EXISTS create_attempts INTEGER NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS last_create_attempt_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS next_recovery_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS recovery_locked_until TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS last_error TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS payments_unknown_recovery_idx
  ON payments (next_recovery_at, id)
  WHERE status = 'pending' AND provider_payment_id = '';
