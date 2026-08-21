-- Finalize catalogue v2 around explicit Ficusin-owned business identities.
-- Migration 055 was an intermediate bridge: it made legacy slug/SKU values
-- numeric. From here on `product_code` is the public PRODUCT identity and
-- `product_variants.sku` is the sellable SKU identity. External IDs remain
-- mappings only.

-- PRODUCT identity ---------------------------------------------------------
ALTER TABLE products ADD COLUMN IF NOT EXISTS product_code BIGINT;
UPDATE products
SET product_code = slug::BIGINT
WHERE product_code IS NULL AND slug ~ '^[1-9][0-9]*$';

-- Be defensive if this migration is ever rehearsed against data that skipped
-- the intermediate numeric-slug rewrite.
WITH missing AS (
  SELECT id, ROW_NUMBER() OVER (ORDER BY id)
    + COALESCE((SELECT MAX(product_code) FROM products), 0) AS generated
  FROM products
  WHERE product_code IS NULL
)
UPDATE products p SET product_code = missing.generated
FROM missing WHERE p.id = missing.id;

ALTER TABLE products ALTER COLUMN product_code SET NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS products_product_code_uidx ON products(product_code);

-- 055 temporarily used this sequence as the legacy slug default. PostgreSQL
-- refuses to drop a sequence while that DEFAULT still depends on it, so detach
-- slug first and then reuse the sequence name for the real product_code.
ALTER TABLE products ALTER COLUMN slug DROP DEFAULT;
DROP SEQUENCE IF EXISTS ficusin_product_code_seq;
CREATE SEQUENCE ficusin_product_code_seq;
SELECT setval(
  'ficusin_product_code_seq',
  COALESCE((SELECT MAX(product_code) FROM products), 0) + 1,
  FALSE
);
ALTER TABLE products ALTER COLUMN product_code
  SET DEFAULT nextval('ficusin_product_code_seq');

DROP TRIGGER IF EXISTS products_immutable_code ON products;
CREATE OR REPLACE FUNCTION prevent_product_code_change() RETURNS trigger AS $$
BEGIN
  IF OLD.product_code IS DISTINCT FROM NEW.product_code THEN
    RAISE EXCEPTION 'Ficusin product_code is immutable';
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER products_immutable_code
BEFORE UPDATE OF product_code ON products
FOR EACH ROW EXECUTE FUNCTION prevent_product_code_change();

-- SKU identity -------------------------------------------------------------
-- 055 already converted all SKU values to positive decimal strings. Keep a
-- dedicated immutable trigger so PRODUCT and SKU identity do not share logic.
DROP TRIGGER IF EXISTS product_variants_immutable_sku ON product_variants;
DROP FUNCTION IF EXISTS prevent_catalogue_identity_change();
CREATE OR REPLACE FUNCTION prevent_variant_sku_change() RETURNS trigger AS $$
BEGIN
  IF OLD.sku IS DISTINCT FROM NEW.sku THEN
    RAISE EXCEPTION 'Ficusin SKU is immutable';
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER product_variants_immutable_sku
BEFORE UPDATE OF sku ON product_variants
FOR EACH ROW EXECUTE FUNCTION prevent_variant_sku_change();

-- Orders -------------------------------------------------------------------
-- Historical rows must be self-contained. `product_id` used to be TEXT and
-- held a slug (then briefly a SKU in 055); replace it with the real PRODUCT
-- FK and snapshot the SKU/variant label/specification at purchase time.
--
-- A very old order may reference a catalogue row that was deleted years ago.
-- Never invent a new PRODUCT/SKU for such history: keep the original textual
-- reference in `legacy_product_ref`. New writes do not set that field and are
-- protected by a CHECK requiring the complete PRODUCT + variant + SKU tuple.
ALTER TABLE order_items ADD COLUMN IF NOT EXISTS product_ref_id BIGINT;
ALTER TABLE order_items ADD COLUMN IF NOT EXISTS sku TEXT;
ALTER TABLE order_items ADD COLUMN IF NOT EXISTS legacy_product_ref TEXT;
ALTER TABLE order_items ADD COLUMN IF NOT EXISTS variant_label TEXT NOT NULL DEFAULT '';
ALTER TABLE order_items ADD COLUMN IF NOT EXISTS variant_snapshot JSONB NOT NULL DEFAULT '{}'::JSONB;

UPDATE order_items item
SET product_ref_id = variant.product_id,
    sku = COALESCE(item.sku, variant.sku),
    variant_label = CASE WHEN item.variant_label = '' THEN variant.label ELSE item.variant_label END,
    variant_snapshot = CASE
      WHEN item.variant_snapshot = '{}'::JSONB THEN jsonb_strip_nulls(jsonb_build_object(
        'heightCm', variant.height_cm,
        'potDiameterCm', variant.pot_diameter_cm
      ))
      ELSE item.variant_snapshot
    END
FROM product_variants variant
WHERE variant.id = item.variant_id;

-- 055 can only resolve a historical line while its old catalogue product is
-- still present. Preserve the original reference for anything left unresolved
-- instead of aborting the whole production startup or fabricating an SKU.
UPDATE order_items item
SET legacy_product_ref = item.product_id
WHERE item.legacy_product_ref IS NULL
  AND (item.product_ref_id IS NULL OR item.variant_id IS NULL OR item.sku IS NULL);

ALTER TABLE order_items DROP COLUMN product_id;
ALTER TABLE order_items RENAME COLUMN product_ref_id TO product_id;

ALTER TABLE order_items DROP CONSTRAINT IF EXISTS order_items_catalog_identity_check;
ALTER TABLE order_items ADD CONSTRAINT order_items_catalog_identity_check CHECK (
  legacy_product_ref IS NOT NULL
  OR (product_id IS NOT NULL AND variant_id IS NOT NULL AND sku IS NOT NULL)
);

ALTER TABLE order_items
  ADD CONSTRAINT order_items_product_fk FOREIGN KEY(product_id) REFERENCES products(id) ON DELETE RESTRICT;
CREATE INDEX IF NOT EXISTS order_items_product_idx ON order_items(product_id);
CREATE INDEX IF NOT EXISTS order_items_sku_idx ON order_items(sku);
CREATE INDEX IF NOT EXISTS order_items_legacy_product_ref_idx
  ON order_items(legacy_product_ref) WHERE legacy_product_ref IS NOT NULL;

-- Reviews stay attached to PRODUCT but remember which purchased SKU created
-- the verified-purchase context. Unresolved legacy lines intentionally do not
-- qualify as a verified PRODUCT/SKU purchase because their identity is unknown.
ALTER TABLE product_reviews ADD COLUMN IF NOT EXISTS purchased_sku TEXT;
UPDATE product_reviews review
SET purchased_sku = variant.sku
FROM product_variants variant
WHERE variant.id = review.variant_id AND review.purchased_sku IS NULL;

-- Attribute definitions ----------------------------------------------------
ALTER TABLE attribute_definitions
  ADD COLUMN IF NOT EXISTS is_global BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT '';

-- `options` was the compatibility JSON store. 055 normalized the values;
-- validation/admin code must now use attribute_options. Keep no second source
-- of truth after the conversion.
ALTER TABLE attribute_definitions DROP COLUMN IF EXISTS options;

-- Variant values can be edited directly. Remove the bridge that made legacy
-- physical columns silently become the source of truth again. Existing
-- columns are retained for delivery integrations during this release, but all
-- PIM reads/writes use variant_attribute_values.
DROP TRIGGER IF EXISTS product_variants_sync_attributes ON product_variants;
DROP FUNCTION IF EXISTS sync_variant_catalog_attributes();

-- Variant lifecycle --------------------------------------------------------
ALTER TABLE product_variants
  ADD COLUMN IF NOT EXISTS archived_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS product_variants_product_active_idx
  ON product_variants(product_id, is_active, archived_at, id);

-- Dynamic marketing collections ------------------------------------------
ALTER TABLE collections ADD COLUMN IF NOT EXISTS mode TEXT NOT NULL DEFAULT 'manual';
ALTER TABLE collections ADD COLUMN IF NOT EXISTS rules JSONB NOT NULL DEFAULT '[]'::JSONB;
ALTER TABLE collections DROP CONSTRAINT IF EXISTS collections_mode_check;
ALTER TABLE collections ADD CONSTRAINT collections_mode_check
  CHECK(mode IN ('manual','dynamic'));

-- A rule is intentionally JSON here: unlike attribute options/value storage,
-- collection predicates are an expression tree that will evolve (AND/OR,
-- ranges, membership). It is configuration, not an entity list.

-- Helpful integrity indexes ------------------------------------------------
CREATE INDEX IF NOT EXISTS category_attributes_category_sort_idx
  ON category_attributes(category_id, sort_order, attribute_id);
CREATE INDEX IF NOT EXISTS product_attribute_values_product_idx
  ON product_attribute_values(product_id, attribute_id);
CREATE INDEX IF NOT EXISTS product_media_variant_sort_idx
  ON product_media(variant_id, sort_order, id) WHERE variant_id IS NOT NULL;

-- Slug is no longer public/business identity. Existing internal integrations
-- may still carry it during this deployment, so keep the column temporarily
-- but remove the numeric-code constraint introduced by the bridge. The legacy
-- DEFAULT was already removed before recycling ficusin_product_code_seq above.
-- Application code after this migration must use product_code.
ALTER TABLE products DROP CONSTRAINT IF EXISTS products_code_numeric;
