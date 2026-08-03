ALTER TABLE products
    ADD COLUMN IF NOT EXISTS catalog_section TEXT NOT NULL DEFAULT 'plants',
    ADD COLUMN IF NOT EXISTS plant_kind TEXT,
    ADD COLUMN IF NOT EXISTS light_level TEXT,
    ADD COLUMN IF NOT EXISTS watering TEXT,
    ADD COLUMN IF NOT EXISTS height_class TEXT,
    ADD COLUMN IF NOT EXISTS care_level TEXT,
    ADD COLUMN IF NOT EXISTS placement TEXT,
    ADD COLUMN IF NOT EXISTS pet_safety TEXT,
    ADD COLUMN IF NOT EXISTS growth_habit TEXT;

UPDATE products SET plant_kind = CASE
    WHEN LOWER(name) LIKE 'аглаонема%' THEN 'aglaonema'
    WHEN LOWER(name) LIKE 'алоказия%' THEN 'alocasia'
    WHEN LOWER(name) LIKE 'ананас%' THEN 'pineapple'
    WHEN LOWER(name) LIKE 'бонсай%' THEN 'bonsai'
    ELSE plant_kind END
WHERE plant_kind IS NULL;

CREATE INDEX IF NOT EXISTS products_catalog_section_idx ON products (catalog_section, status);
CREATE INDEX IF NOT EXISTS products_plant_kind_idx ON products (plant_kind) WHERE plant_kind IS NOT NULL;
