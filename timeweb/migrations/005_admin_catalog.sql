ALTER TABLE admin_users
  ALTER COLUMN email DROP NOT NULL;

ALTER TABLE admin_users
  ADD COLUMN IF NOT EXISTS customer_id BIGINT REFERENCES customers(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT TRUE,
  ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP;

CREATE UNIQUE INDEX IF NOT EXISTS admin_users_customer_unique
  ON admin_users(customer_id) WHERE customer_id IS NOT NULL;

ALTER TABLE products
  ADD COLUMN IF NOT EXISTS override_fields TEXT[] NOT NULL DEFAULT '{}',
  ADD COLUMN IF NOT EXISTS saby_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE product_variants
  ADD COLUMN IF NOT EXISTS package_length_cm INTEGER,
  ADD COLUMN IF NOT EXISTS package_width_cm INTEGER,
  ADD COLUMN IF NOT EXISTS package_height_cm INTEGER,
  ADD COLUMN IF NOT EXISTS package_weight_grams INTEGER,
  ADD COLUMN IF NOT EXISTS override_fields TEXT[] NOT NULL DEFAULT '{}',
  ADD COLUMN IF NOT EXISTS saby_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE TABLE IF NOT EXISTS admin_audit_log (
  id BIGSERIAL PRIMARY KEY,
  actor_customer_id BIGINT REFERENCES customers(id) ON DELETE SET NULL,
  actor_role TEXT NOT NULL,
  action TEXT NOT NULL,
  entity_type TEXT NOT NULL,
  entity_id TEXT NOT NULL,
  before_data JSONB,
  after_data JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS admin_audit_log_entity_idx
  ON admin_audit_log(entity_type, entity_id, created_at DESC);
CREATE INDEX IF NOT EXISTS admin_audit_log_actor_idx
  ON admin_audit_log(actor_customer_id, created_at DESC);
