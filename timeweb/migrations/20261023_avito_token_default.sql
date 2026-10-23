-- PostgreSQL core UUID generation uses a cryptographic random source.
-- Preserve existing URLs, publication switches and manual listing pauses.
ALTER TABLE avito_integration_state ALTER COLUMN feed_token SET DEFAULT
  (replace(gen_random_uuid()::text, '-', '') || replace(gen_random_uuid()::text, '-', ''));
