-- Replaces the "we call the user" OTP flow (otp_codes, left in place but
-- unused) with SMS.ru's "user calls us" flow: we hand out a phone number
-- for the user to call from their own phone, and poll whether that call
-- came in. See backend/internal/integration/smsru.go.

CREATE TABLE IF NOT EXISTS call_checks (
    id BIGSERIAL PRIMARY KEY,
    phone TEXT NOT NULL,
    check_id TEXT NOT NULL,
    call_phone TEXT NOT NULL,
    call_phone_pretty TEXT NOT NULL,
    consumed_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS call_checks_phone_idx
    ON call_checks (phone, created_at DESC);
CREATE INDEX IF NOT EXISTS call_checks_check_id_idx
    ON call_checks (check_id);
