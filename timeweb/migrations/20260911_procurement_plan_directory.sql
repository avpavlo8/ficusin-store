ALTER TABLE procurement_supplier_aliases
  ADD COLUMN IF NOT EXISTS supplier_category TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS expected_unit_price NUMERIC(14,4),
  ADD COLUMN IF NOT EXISTS units_per_package INTEGER CHECK (units_per_package > 0);

-- Plans created before this migration already contain the values entered by
-- the buyer. Promote their latest line into the reusable supplier directory.
WITH latest AS (
  SELECT DISTINCT ON (o.supplier_id, LOWER(l.raw_name), COALESCE(l.supplier_article, ''),
      COALESCE(l.pot_diameter_cm, -1), COALESCE(l.height_cm, -1))
    o.supplier_id, l.raw_name, LOWER(REGEXP_REPLACE(TRIM(l.raw_name), '\s+', ' ', 'g')) normalized_name,
    l.supplier_article, l.pot_diameter_cm, l.height_cm, l.saby_id,
    CASE WHEN l.saby_id IS NULL THEN 'new_product' ELSE 'confirmed' END match_status,
    l.supplier_category, l.expected_unit_price, l.units_per_package, o.created_at
  FROM procurement_order_lines l
  JOIN procurement_orders o ON o.id=l.procurement_order_id
  WHERE o.status <> 'cancelled' AND TRIM(l.raw_name) <> ''
  ORDER BY o.supplier_id, LOWER(l.raw_name), COALESCE(l.supplier_article, ''),
    COALESCE(l.pot_diameter_cm, -1), COALESCE(l.height_cm, -1), o.created_at DESC, l.id DESC
)
INSERT INTO procurement_supplier_aliases (
  supplier_id, raw_name, normalized_name, supplier_article, pot_diameter_cm, height_cm,
  matched_saby_id, match_status, supplier_category, expected_unit_price, units_per_package,
  occurrences, last_seen_at
)
SELECT supplier_id, raw_name, normalized_name, supplier_article, pot_diameter_cm, height_cm,
  saby_id, match_status, supplier_category, expected_unit_price, units_per_package, 1, created_at::DATE
FROM latest
ON CONFLICT DO NOTHING;

UPDATE procurement_supplier_aliases alias SET
  supplier_category=latest.supplier_category,
  expected_unit_price=latest.expected_unit_price,
  units_per_package=latest.units_per_package,
  matched_saby_id=COALESCE(alias.matched_saby_id, latest.saby_id),
  match_status=CASE WHEN alias.matched_saby_id IS NULL AND latest.saby_id IS NOT NULL THEN 'confirmed' ELSE alias.match_status END,
  updated_at=CURRENT_TIMESTAMP
FROM (
  SELECT DISTINCT ON (o.supplier_id, LOWER(l.raw_name), COALESCE(l.supplier_article, ''),
      COALESCE(l.pot_diameter_cm, -1), COALESCE(l.height_cm, -1))
    o.supplier_id, l.raw_name, l.supplier_article, l.pot_diameter_cm, l.height_cm,
    l.saby_id, l.supplier_category, l.expected_unit_price, l.units_per_package
  FROM procurement_order_lines l JOIN procurement_orders o ON o.id=l.procurement_order_id
  WHERE o.status <> 'cancelled' AND TRIM(l.raw_name) <> ''
  ORDER BY o.supplier_id, LOWER(l.raw_name), COALESCE(l.supplier_article, ''),
    COALESCE(l.pot_diameter_cm, -1), COALESCE(l.height_cm, -1), o.created_at DESC, l.id DESC
) latest
WHERE alias.supplier_id=latest.supplier_id AND LOWER(alias.raw_name)=LOWER(latest.raw_name)
  AND COALESCE(alias.supplier_article,'')=COALESCE(latest.supplier_article,'')
  AND COALESCE(alias.pot_diameter_cm,-1)=COALESCE(latest.pot_diameter_cm,-1)
  AND COALESCE(alias.height_cm,-1)=COALESCE(latest.height_cm,-1);
