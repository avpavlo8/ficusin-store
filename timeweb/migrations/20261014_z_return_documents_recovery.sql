ALTER TABLE return_intakes ADD COLUMN document_number BIGSERIAL;
ALTER TABLE return_intakes ADD COLUMN deleted_at TIMESTAMPTZ;
ALTER TABLE return_intakes ADD COLUMN deleted_by BIGINT REFERENCES customers(id);
ALTER TABLE marketplace_returns ADD COLUMN deleted_at TIMESTAMPTZ;
ALTER TABLE marketplace_returns ADD COLUMN deleted_by BIGINT REFERENCES customers(id);
ALTER TABLE marketplace_returns ADD COLUMN retail_price_minor_snapshot BIGINT;
ALTER TABLE marketplace_returns ADD COLUMN discount_percent INTEGER NOT NULL DEFAULT 0 CHECK(discount_percent BETWEEN 0 AND 100);
UPDATE marketplace_returns r SET retail_price_minor_snapshot=v.base_price_minor FROM product_variants v WHERE v.id=r.canonical_variant_id;
CREATE INDEX marketplace_returns_discount_idx ON marketplace_returns(id) WHERE condition='restoring' AND discount_percent>0 AND deleted_at IS NULL;
