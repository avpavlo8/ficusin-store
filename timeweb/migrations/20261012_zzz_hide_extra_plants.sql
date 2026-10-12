-- Launch assortment approved by the owner: hide the 26 plant SKU absent
-- from "Каталог (1).pdf". Twenty-four cards contain only that SKU, while
-- cactus 476/500 and syngonium 496/482 share cards with included PDF SKU.
-- Draft only the 24 singleton cards. The two mixed-card variants are hidden
-- solely from the storefront; Saby/marketplace links and activity stay intact.
CREATE TEMP TABLE launch_extra_plant_expected (
  product_id BIGINT PRIMARY KEY,
  excluded_sku TEXT NOT NULL UNIQUE,
  expected_name TEXT NOT NULL,
  included_sibling_sku TEXT
) ON COMMIT DROP;
INSERT INTO launch_extra_plant_expected(product_id, excluded_sku, expected_name, included_sibling_sku) VALUES
  (11080, '545', 'Бонсай Араукария', NULL),
  (11078, '543', 'Бонсай Метасеквойя «Лес»', NULL),
  (11176, '641', 'Бонсай Микс', NULL),
  (11015, '480', 'Глоксиния в горшке', NULL),
  (11014, '479', 'Даваллия', NULL),
  (11272, '737', 'Диффенбахия', NULL),
  (11101, '566', 'Замиокулькас', NULL),
  (11011, '476', 'Кактус', '500'),
  (11329, '794', 'Композиция из растений со Спатифиллумом', NULL),
  (11125, '590', 'Композиция с Каллой и Аглаонемой', NULL),
  (11062, '527', 'Композиция с алоказией', NULL),
  (11025, '490', 'Кротон (Кодиеум)', NULL),
  (11323, '788', 'Лилии', NULL),
  (11364, '829', 'Орхидея Фаленопсис крашеный', NULL),
  (11052, '517', 'Пахира', NULL),
  (11284, '749', 'Сансевиерия цилиндрика «Спагетти»', NULL),
  (11017, '496', 'Сингониум', '482'),
  (11115, '580', 'Спирея', NULL),
  (11034, '499', 'Стрелиция Николая', NULL),
  (11146, '611', 'Туя западная', NULL),
  (11359, '824', 'Фаленопсис (орхидея) в кашпо', NULL),
  (11304, '769', 'Филодендрон Ring of Fire', NULL),
  (11113, '578', 'Флебодиум Блю Стар', NULL),
  (11042, '507', 'Хедера (плющ)', NULL),
  (11046, '511', 'Цикламен', NULL),
  (11147, '612', 'Чубушник венечный Шнеештурм', NULL);

DO $$
DECLARE matched_count INTEGER;
BEGIN
  -- A fresh test or installation database may have products, but no target
  -- launch identities. A partial target set must still fail closed.
  IF NOT EXISTS (
    SELECT 1 FROM launch_extra_plant_expected e
    JOIN products p ON p.id=e.product_id
  ) THEN
    RETURN;
  END IF;
  SELECT COUNT(*) INTO matched_count
  FROM launch_extra_plant_expected e
  JOIN products p ON p.id = e.product_id
  JOIN product_variants v ON v.product_id = p.id AND v.sku = e.excluded_sku
  WHERE p.catalog_section = 'plants' AND p.status = 'published'
    AND p.name = e.expected_name AND v.is_active = 1 AND v.archived_at IS NULL
    AND ((e.included_sibling_sku IS NULL AND NOT EXISTS (
           SELECT 1 FROM product_variants other WHERE other.product_id = p.id AND other.id <> v.id
         )) OR (e.included_sibling_sku IS NOT NULL
           AND EXISTS (SELECT 1 FROM product_variants sibling
                       WHERE sibling.product_id = p.id AND sibling.sku = e.included_sibling_sku
                         AND sibling.is_active = 1 AND sibling.archived_at IS NULL)
           AND NOT EXISTS (SELECT 1 FROM product_variants other
                           WHERE other.product_id = p.id AND other.id <> v.id
                             AND other.sku <> e.included_sibling_sku)));
  IF matched_count <> 26 THEN
    RAISE EXCEPTION 'launch extra plant identity/status mismatch: expected 26, got %', matched_count;
  END IF;
END $$;

CREATE TABLE IF NOT EXISTS launch_hidden_extra_plant_products (
  product_id BIGINT PRIMARY KEY REFERENCES products(id) ON DELETE CASCADE,
  excluded_sku TEXT NOT NULL,
  previous_status TEXT NOT NULL,
  hidden_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS launch_hidden_extra_plant_variants (
  variant_id BIGINT PRIMARY KEY REFERENCES product_variants(id) ON DELETE CASCADE,
  product_id BIGINT NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  excluded_sku TEXT NOT NULL,
  hidden_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO launch_hidden_extra_plant_products(product_id, excluded_sku, previous_status)
SELECT p.id, e.excluded_sku, p.status
FROM launch_extra_plant_expected e JOIN products p ON p.id = e.product_id
WHERE e.included_sibling_sku IS NULL
ON CONFLICT (product_id) DO NOTHING;

INSERT INTO launch_hidden_extra_plant_variants(variant_id, product_id, excluded_sku)
SELECT v.id, p.id, e.excluded_sku
FROM launch_extra_plant_expected e JOIN products p ON p.id = e.product_id
JOIN product_variants v ON v.product_id = p.id AND v.sku = e.excluded_sku
WHERE e.included_sibling_sku IS NOT NULL
ON CONFLICT (variant_id) DO NOTHING;

UPDATE products p
SET status = 'draft',
    override_fields = ARRAY(SELECT DISTINCT unnest(p.override_fields || ARRAY['status']::TEXT[])),
    updated_at = CURRENT_TIMESTAMP
FROM launch_hidden_extra_plant_products h
WHERE p.id = h.product_id AND p.status = 'published';
