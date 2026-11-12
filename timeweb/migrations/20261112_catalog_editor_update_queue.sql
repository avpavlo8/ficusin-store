-- Keep the latest edit while a prior request is in flight. Its receipt remains
-- intact until the worker can safely promote the next snapshot.
ALTER TABLE catalog_editor_publications
 ADD COLUMN IF NOT EXISTS pending_payload JSONB,
 ADD COLUMN IF NOT EXISTS pending_fingerprint TEXT;
