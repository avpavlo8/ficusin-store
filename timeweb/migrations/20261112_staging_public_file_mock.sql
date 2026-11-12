-- Separate public-image test namespace. Private mock objects are never served
-- by the staging image route; only products/ and collections/ image keys exist.
CREATE TABLE IF NOT EXISTS public_file_mock_objects(
 object_key TEXT PRIMARY KEY CHECK(object_key LIKE 'products/%' OR object_key LIKE 'collections/%'),
 content_type TEXT NOT NULL CHECK(content_type IN ('image/jpeg','image/png','image/webp','image/gif')),
 body BYTEA NOT NULL CHECK(octet_length(body)>0 AND octet_length(body)<=67108864),
 sha256 TEXT NOT NULL CHECK(sha256 ~ '^[a-f0-9]{64}$'),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
