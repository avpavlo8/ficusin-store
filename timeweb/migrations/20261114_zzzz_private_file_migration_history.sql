-- Evidence for verified copies. Source bytea and original public objects remain
-- intact; this table must outlive retirement of compatibility readers.
CREATE TABLE file_asset_migration_history (
    id BIGSERIAL PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('avatars','returns','procurement','chat','reviews')),
    source_id BIGINT NOT NULL,
    source_scope TEXT NOT NULL CHECK (source_scope IN ('bytea','legacy-public')),
    source_key TEXT,
    destination_key TEXT NOT NULL,
    byte_count BIGINT NOT NULL CHECK (byte_count > 0 AND byte_count <= 67108864),
    sha256 TEXT NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (kind, source_id, destination_key),
    CHECK ((source_scope='legacy-public') = (source_key IS NOT NULL))
);
