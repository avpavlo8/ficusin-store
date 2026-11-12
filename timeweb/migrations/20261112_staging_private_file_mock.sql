-- Explicit FILE_STORAGE_MODE=mock test-provider data, never a production
-- bytea fallback. Persisting objects in the isolated staging database keeps
-- synthetic uploads usable across application deployments/restarts.
CREATE TABLE IF NOT EXISTS private_file_mock_objects(
 object_key TEXT PRIMARY KEY,
 content_type TEXT NOT NULL,
 body BYTEA NOT NULL CHECK(octet_length(body)>0 AND octet_length(body)<=67108864),
 sha256 TEXT NOT NULL CHECK(sha256 ~ '^[a-f0-9]{64}$'),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
