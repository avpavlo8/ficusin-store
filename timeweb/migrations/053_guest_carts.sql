-- Anonymous shoppers keep only an opaque token in an HttpOnly cookie. The
-- basket itself lives here, so a deploy or a cleared JS cache cannot restore
-- an obsolete browser copy.
CREATE TABLE IF NOT EXISTS guest_carts (
    token_hash TEXT PRIMARY KEY,
    items JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS guest_carts_expires_at_idx ON guest_carts (expires_at);
