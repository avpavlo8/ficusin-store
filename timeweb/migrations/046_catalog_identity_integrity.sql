-- Close integrity gaps left deliberately open during the compatibility
-- rollout. No product/order/URL identity is rewritten by this migration.

-- NULL does not compare equal in a regular UNIQUE constraint. Keep the
-- oldest identical product-level row if a previous retry created duplicates,
-- then make that business key genuinely unique.
DELETE FROM product_external_ids duplicate
USING product_external_ids keeper
WHERE duplicate.id > keeper.id
  AND duplicate.product_id = keeper.product_id
  AND duplicate.variant_id IS NULL AND keeper.variant_id IS NULL
  AND duplicate.provider = keeper.provider
  AND duplicate.id_type = keeper.id_type;

CREATE UNIQUE INDEX IF NOT EXISTS product_external_ids_product_level_uidx
  ON product_external_ids(product_id,provider,id_type)
  WHERE variant_id IS NULL;

ALTER TABLE product_external_ids DROP CONSTRAINT IF EXISTS product_external_ids_nonempty;
ALTER TABLE product_external_ids ADD CONSTRAINT product_external_ids_nonempty CHECK (
  provider = LOWER(BTRIM(provider)) AND provider <> '' AND
  id_type = LOWER(BTRIM(id_type)) AND id_type <> '' AND
  BTRIM(external_id) <> ''
) NOT VALID;
ALTER TABLE product_external_ids VALIDATE CONSTRAINT product_external_ids_nonempty;

CREATE OR REPLACE FUNCTION validate_external_id_variant_owner() RETURNS trigger AS $$
BEGIN
  IF NEW.variant_id IS NOT NULL AND NOT EXISTS (
    SELECT 1 FROM product_variants WHERE id=NEW.variant_id AND product_id=NEW.product_id
  ) THEN
    RAISE EXCEPTION 'external ID variant does not belong to product';
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS product_external_ids_variant_owner ON product_external_ids;
CREATE CONSTRAINT TRIGGER product_external_ids_variant_owner
AFTER INSERT OR UPDATE OF product_id,variant_id ON product_external_ids
DEFERRABLE INITIALLY IMMEDIATE FOR EACH ROW
EXECUTE FUNCTION validate_external_id_variant_owner();

-- FIC-000001..FIC-999999 is a finite published format. Fail at sequence
-- allocation with an explicit capacity boundary instead of producing a
-- seven-digit SKU that then fails an unrelated CHECK.
ALTER SEQUENCE ficusin_sku_seq MAXVALUE 999999 NO CYCLE;
