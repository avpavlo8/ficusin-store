-- OTP-based login/registration + optional profile fields.

ALTER TABLE customers
    ALTER COLUMN email DROP NOT NULL,
    ALTER COLUMN password_hash DROP NOT NULL;

ALTER TABLE customers
    ADD COLUMN IF NOT EXISTS last_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS patronymic TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS delivery_address TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS otp_codes (
      id BIGSERIAL PRIMARY KEY,
      phone TEXT NOT NULL,
      purpose TEXT NOT NULL DEFAULT 'login' CHECK (purpose IN ('login')),
      code_hash CHAR(64) NOT NULL,
      attempts INTEGER NOT NULL DEFAULT 0,
      max_attempts INTEGER NOT NULL DEFAULT 5,
      expires_at TIMESTAMPTZ NOT NULL,
      consumed_at TIMESTAMPTZ,
      created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
  );

CREATE INDEX IF NOT EXISTS otp_codes_phone_idx
    ON otp_codes (phone, created_at DESC);
CREATE INDEX IF NOT EXISTS otp_codes_expiry_idx
    ON otp_codes (expires_at);
