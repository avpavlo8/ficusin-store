-- Shared marketplace content, channel-specific categories and durable plans.
CREATE SEQUENCE IF NOT EXISTS catalog_editor_barcode_seq;
CREATE OR REPLACE FUNCTION catalog_editor_barcode() RETURNS TEXT LANGUAGE plpgsql AS $$
DECLARE body TEXT; candidate TEXT; serial BIGINT; total INT; i INT;
BEGIN
 LOOP
  serial := nextval('catalog_editor_barcode_seq');
  IF serial>999999999 THEN RAISE EXCEPTION 'Internal barcode range exhausted'; END IF;
  body := '200' || LPAD(serial::TEXT,9,'0'); total := 0;
  FOR i IN 1..12 LOOP total := total + substring(body,i,1)::INT * CASE WHEN i%2=0 THEN 3 ELSE 1 END; END LOOP;
  candidate := body || ((10-total%10)%10)::TEXT;
  IF NOT EXISTS(SELECT 1 FROM product_variants WHERE generated_barcode=candidate)
    AND NOT EXISTS(SELECT 1 FROM saby_nomenclature WHERE barcode=candidate OR candidate=ANY(barcodes))
    AND NOT EXISTS(SELECT 1 FROM product_external_ids WHERE id_type IN ('barcode','ean','gtin') AND external_id=candidate) THEN RETURN candidate; END IF;
 END LOOP;
END $$;
ALTER TABLE product_variants ADD COLUMN IF NOT EXISTS generated_barcode TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS variant_generated_barcode_unique ON product_variants(generated_barcode) WHERE generated_barcode IS NOT NULL;
CREATE TABLE IF NOT EXISTS catalog_editor_plans (
 product_id BIGINT PRIMARY KEY REFERENCES products(id) ON DELETE CASCADE,
 config JSONB NOT NULL DEFAULT '{}'::JSONB,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE IF NOT EXISTS catalog_editor_references (
 channel TEXT NOT NULL CHECK(channel IN ('ozon','wb','saby')),
 kind TEXT NOT NULL,
 scope TEXT NOT NULL DEFAULT '',
 payload JSONB NOT NULL,
 fetched_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 PRIMARY KEY(channel,kind,scope)
);
CREATE TABLE IF NOT EXISTS catalog_editor_publications (
 variant_id BIGINT NOT NULL REFERENCES product_variants(id) ON DELETE RESTRICT,
 channel TEXT NOT NULL CHECK(channel IN ('ozon','wb','saby')),
 fingerprint TEXT NOT NULL,
 payload JSONB NOT NULL,
 status TEXT NOT NULL DEFAULT 'prepared',
 message TEXT NOT NULL DEFAULT '',
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 PRIMARY KEY(variant_id,channel)
);
