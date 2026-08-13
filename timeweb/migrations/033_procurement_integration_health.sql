-- Safe connection checks record only status and sanitized errors. Credentials
-- remain in Timeweb environment variables and never enter PostgreSQL.
CREATE TABLE IF NOT EXISTS procurement_integration_health (
  channel TEXT PRIMARY KEY CHECK (channel IN ('saby', 'wb', 'ozon')),
  last_checked_at TIMESTAMPTZ,
  last_success_at TIMESTAMPTZ,
  last_error TEXT NOT NULL DEFAULT ''
);

INSERT INTO procurement_integration_health (channel)
VALUES ('saby'), ('wb'), ('ozon')
ON CONFLICT (channel) DO NOTHING;
