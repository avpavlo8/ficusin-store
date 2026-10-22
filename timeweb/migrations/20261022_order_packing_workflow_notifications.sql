-- Notification channels never gate preparation. Keep delivery durable per browser.
ALTER TABLE order_packing_workflows ADD COLUMN IF NOT EXISTS prepared_at TIMESTAMPTZ;
ALTER TABLE order_packing_workflows ADD COLUMN IF NOT EXISTS notified_at TIMESTAMPTZ;
UPDATE order_packing_workflows w SET notified_at=b.sent_at,prepared_at=COALESCE(b.sent_at,w.updated_at)
FROM outbox b WHERE b.order_packing_id=w.id AND w.notified_at IS NULL AND b.sent_at IS NOT NULL;
CREATE TABLE order_packing_push_outbox (
 id BIGSERIAL PRIMARY KEY,
 workflow_id BIGINT NOT NULL REFERENCES order_packing_workflows(id) ON DELETE CASCADE,
 subscription_id BIGINT REFERENCES push_subscriptions(id) ON DELETE SET NULL,
 attempts INTEGER NOT NULL DEFAULT 0,
 last_error TEXT NOT NULL DEFAULT '',
 next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 sent_at TIMESTAMPTZ,
 cancelled_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 UNIQUE(workflow_id,subscription_id)
);
CREATE INDEX order_packing_push_pending_idx ON order_packing_push_outbox(next_attempt_at)
WHERE sent_at IS NULL AND cancelled_at IS NULL AND attempts<5;
