-- Existing site cards no longer subscribe to Saby content or prices. Archive
-- every changed array, including flags left on old unlinked products. Preserve
-- the stock opt-in exactly; identity, content, prices and media stay untouched.
LOCK TABLE products IN ACCESS EXCLUSIVE MODE;
CREATE SCHEMA IF NOT EXISTS legacy_archive;
CREATE TABLE legacy_archive.product_saby_content_flags AS
 SELECT id AS product_id,saby_fields,updated_at AS product_updated_at,
  CURRENT_TIMESTAMP AS archived_at
 FROM products
 WHERE saby_fields IS DISTINCT FROM CASE WHEN 'stock'=ANY(saby_fields)
  THEN ARRAY['stock']::TEXT[] ELSE ARRAY[]::TEXT[] END;
ALTER TABLE legacy_archive.product_saby_content_flags ADD PRIMARY KEY(product_id);
COMMENT ON TABLE legacy_archive.product_saby_content_flags IS
 'Original Saby subscriptions before retiring content/price flags; stock opt-in preserved in products';
UPDATE products SET saby_fields=CASE WHEN 'stock'=ANY(saby_fields)
 THEN ARRAY['stock']::TEXT[] ELSE ARRAY[]::TEXT[] END
 WHERE saby_fields IS DISTINCT FROM CASE WHEN 'stock'=ANY(saby_fields)
  THEN ARRAY['stock']::TEXT[] ELSE ARRAY[]::TEXT[] END;
