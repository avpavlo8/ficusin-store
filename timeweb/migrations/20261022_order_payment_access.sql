-- A readable order number is not a credential. Legacy guests must verify their phone.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS guest_payment_token_hash TEXT NOT NULL DEFAULT '';
