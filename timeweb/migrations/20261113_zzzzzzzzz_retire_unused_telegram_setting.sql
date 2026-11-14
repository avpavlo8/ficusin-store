-- This switch never gated a Telegram sender. Removing it does not change
-- delivery: production credentials and the runtime environment select the
-- sender. Preserve the misleading preference as history, outside active config.
LOCK TABLE settings IN ACCESS EXCLUSIVE MODE;
CREATE SCHEMA IF NOT EXISTS legacy_archive;
CREATE TABLE legacy_archive.retired_settings AS
 SELECT * FROM settings WHERE key='telegram.enabled';
ALTER TABLE legacy_archive.retired_settings ADD PRIMARY KEY(key);
COMMENT ON TABLE legacy_archive.retired_settings IS 'Historical preferences without runtime consumers; original values and timestamps preserved';
DELETE FROM settings WHERE key='telegram.enabled';
-- Cached old owners must not recreate an inert setting after retirement.
ALTER TABLE settings ADD CONSTRAINT settings_retired_telegram_key_check
 CHECK(key<>'telegram.enabled');
