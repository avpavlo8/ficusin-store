-- Only a verified provider result can settle a refund.
ALTER TABLE payment_refunds ADD COLUMN IF NOT EXISTS provider_refund_id TEXT NOT NULL DEFAULT '';
ALTER TABLE payment_refunds ADD COLUMN IF NOT EXISTS actor_id BIGINT REFERENCES customers(id);
ALTER TABLE payment_refunds ADD COLUMN IF NOT EXISTS request_payload JSONB;
ALTER TABLE payment_refunds ADD COLUMN IF NOT EXISTS last_error TEXT NOT NULL DEFAULT '';
ALTER TABLE payment_refunds ADD COLUMN IF NOT EXISTS needs_review BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE payment_refunds ADD COLUMN IF NOT EXISTS receipt_required BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE payment_refunds ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE payment_refunds ALTER COLUMN status SET DEFAULT 'pending';
CREATE UNIQUE INDEX IF NOT EXISTS payment_refunds_provider_id_idx ON payment_refunds(provider_refund_id) WHERE provider_refund_id<>'';
CREATE INDEX IF NOT EXISTS payment_refunds_pending_idx ON payment_refunds(updated_at) WHERE status='pending';

ALTER TABLE payment_refunds ADD COLUMN IF NOT EXISTS receipt_confirmed_by BIGINT REFERENCES customers(id);
ALTER TABLE payment_refunds ADD COLUMN IF NOT EXISTS resolved_by BIGINT REFERENCES customers(id);
ALTER TABLE payment_refunds ADD COLUMN IF NOT EXISTS request_id TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX IF NOT EXISTS payment_refunds_request_part_idx ON payment_refunds(order_id,request_id,payment_id) WHERE request_id<>'';
