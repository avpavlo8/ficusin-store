-- Private, durable customer/support conversations. Guest tokens are stored hashed.
CREATE TABLE support_conversations (
    id BIGSERIAL PRIMARY KEY,
    customer_id BIGINT REFERENCES customers(id) ON DELETE SET NULL,
    guest_hash TEXT UNIQUE,
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','closed')),
    customer_read_id BIGINT NOT NULL DEFAULT 0,
    staff_read_id BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX support_customer_idx ON support_conversations(customer_id) WHERE customer_id IS NOT NULL;
CREATE TABLE support_messages (
    id BIGSERIAL PRIMARY KEY,
    conversation_id BIGINT NOT NULL REFERENCES support_conversations(id) ON DELETE CASCADE,
    sender TEXT NOT NULL CHECK (sender IN ('customer','staff')),
    staff_id BIGINT REFERENCES customers(id) ON DELETE SET NULL,
    client_id TEXT NOT NULL,
    body TEXT NOT NULL DEFAULT '',
    context_url TEXT NOT NULL DEFAULT '',
    context_label TEXT NOT NULL DEFAULT '',
    image_type TEXT NOT NULL DEFAULT '',
    image_data BYTEA,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(conversation_id,sender,client_id)
);
CREATE INDEX support_messages_history_idx ON support_messages(conversation_id,id DESC);
CREATE INDEX support_inbox_idx ON support_conversations(status,updated_at DESC);

CREATE TABLE support_consents (
    id BIGSERIAL PRIMARY KEY,
    conversation_id BIGINT NOT NULL REFERENCES support_conversations(id) ON DELETE CASCADE,
    accepted_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    document_version TEXT NOT NULL,
    ip_address TEXT NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT ''
);
