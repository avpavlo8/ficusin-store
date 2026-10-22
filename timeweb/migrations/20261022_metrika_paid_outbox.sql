CREATE TABLE metrika_paid_outbox (
 order_id BIGINT PRIMARY KEY REFERENCES orders(id) ON DELETE CASCADE,
 event_id UUID NOT NULL UNIQUE DEFAULT gen_random_uuid(),
 counter_id TEXT NOT NULL, client_id TEXT NOT NULL,
 products JSONB NOT NULL DEFAULT '[]'::jsonb,
 amount NUMERIC(14,2) NOT NULL CHECK (amount > 0),
 status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','sending','sent','failed','uncertain','cancelled')),
 attempts INTEGER NOT NULL DEFAULT 0,
 next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 claimed_at TIMESTAMPTZ, sent_at TIMESTAMPTZ,
 last_error TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX metrika_paid_outbox_pending_idx ON metrika_paid_outbox(next_attempt_at) WHERE status='pending';
