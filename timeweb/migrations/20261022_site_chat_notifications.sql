CREATE TABLE support_push_subscriptions (
    id BIGSERIAL PRIMARY KEY,
    endpoint TEXT NOT NULL,
    p256dh TEXT NOT NULL,
    auth TEXT NOT NULL,
    customer_id BIGINT REFERENCES customers(id) ON DELETE CASCADE,
    guest_hash TEXT NOT NULL DEFAULT '',
    session_hash TEXT NOT NULL DEFAULT '',
    staff BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(endpoint, staff),
    CHECK (customer_id IS NOT NULL OR guest_hash <> ''),
    CHECK (NOT staff OR customer_id IS NOT NULL)
);
CREATE INDEX support_push_recipient_idx ON support_push_subscriptions(customer_id,guest_hash,staff);
CREATE TABLE support_push_deliveries (
    id BIGSERIAL PRIMARY KEY,
    message_id BIGINT NOT NULL REFERENCES support_messages(id) ON DELETE CASCADE,
    subscription_id BIGINT NOT NULL REFERENCES support_push_subscriptions(id) ON DELETE CASCADE,
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at TIMESTAMPTZ,
    outcome TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(message_id,subscription_id)
);
CREATE INDEX support_push_pending_idx ON support_push_deliveries(next_attempt_at,id) WHERE completed_at IS NULL;
