-- Extend existing durable editor publications; never promote disabled jobs.
ALTER TABLE catalog_editor_references DROP CONSTRAINT catalog_editor_references_channel_check;
ALTER TABLE catalog_editor_references ADD CONSTRAINT catalog_editor_references_channel_check CHECK(channel IN ('ozon','wb','saby','flowwow'));
ALTER TABLE catalog_editor_publications DROP CONSTRAINT catalog_editor_publications_channel_check;
ALTER TABLE catalog_editor_publications ADD CONSTRAINT catalog_editor_publications_channel_check CHECK(channel IN ('ozon','wb','saby','flowwow'));
ALTER TABLE catalog_editor_remote_links DROP CONSTRAINT catalog_editor_remote_links_channel_check;
ALTER TABLE catalog_editor_remote_links ADD CONSTRAINT catalog_editor_remote_links_channel_check CHECK(channel IN ('ozon','wb','saby','flowwow'));
CREATE INDEX flowwow_publications_due ON catalog_editor_publications(next_attempt_at) WHERE channel='flowwow' AND status IN ('prepared','queued','processing');
CREATE TABLE flowwow_inventory_sync (
 variant_id BIGINT PRIMARY KEY REFERENCES product_variants(id) ON DELETE CASCADE,
 price_minor BIGINT,
 stock INTEGER,
 offer_id TEXT NOT NULL DEFAULT '',
 shop_id TEXT NOT NULL,
 last_error TEXT NOT NULL DEFAULT '',
 next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 synced_at TIMESTAMPTZ
);
-- Existing identities belong to exactly one shop. A changed env must not silently
-- apply them to a different seller account.
CREATE TABLE flowwow_shop_binding (
 singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK(singleton),
 shop_id TEXT NOT NULL
);
