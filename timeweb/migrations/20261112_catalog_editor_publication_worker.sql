-- Resumable publication receipts. Never discard a receipt after a network failure.
ALTER TABLE catalog_editor_publications
 ADD COLUMN IF NOT EXISTS progress JSONB NOT NULL DEFAULT '{}'::JSONB,
 ADD COLUMN IF NOT EXISTS attempts INTEGER NOT NULL DEFAULT 0,
 ADD COLUMN IF NOT EXISTS next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 ADD COLUMN IF NOT EXISTS lease_until TIMESTAMPTZ,
 ADD COLUMN IF NOT EXISTS execution_mode TEXT NOT NULL DEFAULT 'disabled';
CREATE INDEX IF NOT EXISTS editor_publications_due ON catalog_editor_publications(next_attempt_at) WHERE channel IN ('wb','ozon') AND status IN ('prepared','queued','processing');
CREATE TABLE IF NOT EXISTS catalog_editor_remote_links (
 variant_id BIGINT NOT NULL REFERENCES product_variants(id) ON DELETE RESTRICT,
 channel TEXT NOT NULL CHECK(channel IN ('wb','ozon')),
 execution_mode TEXT NOT NULL CHECK(execution_mode IN ('mock','live')),
 remote_id TEXT NOT NULL,
 offer_id TEXT NOT NULL,
 sku TEXT NOT NULL DEFAULT '',
 barcode TEXT NOT NULL DEFAULT '',
 verified_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 PRIMARY KEY(variant_id,channel,execution_mode),
 UNIQUE(channel,execution_mode,remote_id),
 UNIQUE(channel,execution_mode,offer_id)
);
