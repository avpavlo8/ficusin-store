CREATE TABLE IF NOT EXISTS customers (
  id BIGSERIAL PRIMARY KEY,
  email TEXT NOT NULL,
  phone TEXT NOT NULL,
  password_hash TEXT NOT NULL,
  full_name TEXT NOT NULL,
  account_type TEXT NOT NULL DEFAULT 'retail'
    CHECK (account_type IN ('retail', 'wholesale')),
  wholesale_status TEXT NOT NULL DEFAULT 'not_requested'
    CHECK (wholesale_status IN ('not_requested', 'pending', 'approved', 'rejected')),
  lifetime_spend_minor BIGINT NOT NULL DEFAULT 0,
  retail_discount_bps INTEGER NOT NULL DEFAULT 0,
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  email_verified BOOLEAN NOT NULL DEFAULT FALSE,
  consent_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS customers_email_lower_unique
  ON customers (LOWER(email));
CREATE UNIQUE INDEX IF NOT EXISTS customers_phone_unique
  ON customers (phone);
CREATE INDEX IF NOT EXISTS customers_account_type_idx
  ON customers (account_type, wholesale_status);

CREATE TABLE IF NOT EXISTS organizations (
  id BIGSERIAL PRIMARY KEY,
  name TEXT NOT NULL,
  inn TEXT NOT NULL,
  kpp TEXT,
  legal_address TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending', 'approved', 'rejected')),
  wholesale_discount_bps INTEGER NOT NULL DEFAULT 0,
  payment_terms TEXT NOT NULL DEFAULT 'invoice',
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  approved_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS organizations_inn_unique
  ON organizations (inn);

CREATE TABLE IF NOT EXISTS organization_members (
  id BIGSERIAL PRIMARY KEY,
  organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  customer_id BIGINT NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
  role TEXT NOT NULL DEFAULT 'buyer',
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE (organization_id, customer_id)
);

CREATE TABLE IF NOT EXISTS auth_sessions (
  token_hash CHAR(64) PRIMARY KEY,
  customer_id BIGINT NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  last_seen_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  user_agent TEXT
);

CREATE INDEX IF NOT EXISTS auth_sessions_customer_idx
  ON auth_sessions (customer_id);
CREATE INDEX IF NOT EXISTS auth_sessions_expiry_idx
  ON auth_sessions (expires_at);
