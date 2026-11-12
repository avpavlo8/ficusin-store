-- Isolated local vendor emulator state; never used by live integrations.
CREATE TABLE IF NOT EXISTS catalog_editor_mock_state (
 key TEXT PRIMARY KEY,
 payload JSONB NOT NULL,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
