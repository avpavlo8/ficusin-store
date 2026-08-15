-- Read-only catalogue media diagnostics must stay cheap as galleries grow.
CREATE INDEX IF NOT EXISTS product_media_object_key_idx ON product_media(object_key);
CREATE INDEX IF NOT EXISTS media_mirror_card_url_idx ON media_mirror(card_url) WHERE card_url IS NOT NULL;
