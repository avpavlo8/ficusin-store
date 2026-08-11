-- Small verified directory used for the first end-to-end procurement test.
-- Every alias below was taken from an uploaded Gale ProWay packing list and
-- matched to an existing Saby card by species and pot size. Unknown supplier
-- articles deliberately stay empty: inventing them would make matching less
-- reliable, not more complete.

INSERT INTO procurement_suppliers (name, kind, country_code, default_currency)
SELECT 'Gale ProWay', 'international', 'NL', 'EUR'
WHERE NOT EXISTS (
  SELECT 1 FROM procurement_suppliers WHERE LOWER(name) = LOWER('Gale ProWay')
);

WITH supplier AS (
  SELECT id FROM procurement_suppliers
  WHERE LOWER(name) = LOWER('Gale ProWay')
  ORDER BY id LIMIT 1
), pilot(saby_id) AS (
  VALUES
    ('saby-1125'), -- Бонсай Лигуструм D15
    ('saby-1140'), -- Бонсай Фикус Retusa D15
    ('saby-1117'), -- Бонсай Зелкова D15
    ('saby-1131'), -- Бонсай Подокарпус D15
    ('saby-1173'), -- Сансевиерия Цилиндрика Спагетти
    ('saby-3234'), -- Аглаонема Мария Кристина
    ('saby-3019')  -- Непентес
)
INSERT INTO procurement_supplier_products (
  supplier_id, saby_id, availability_status
)
SELECT supplier.id, pilot.saby_id, 'available'
FROM supplier CROSS JOIN pilot
JOIN saby_nomenclature nomenclature ON nomenclature.saby_id = pilot.saby_id
ON CONFLICT (supplier_id, saby_id) DO NOTHING;

WITH supplier AS (
  SELECT id FROM procurement_suppliers
  WHERE LOWER(name) = LOWER('Gale ProWay')
  ORDER BY id LIMIT 1
), aliases(
  raw_name, pot_diameter_cm, height_cm, saby_id, occurrences, last_seen_at
) AS (
  VALUES
    ('Bonsai Ligustrum Sinense In Ceramic', 15.0, 30.0, 'saby-1125', 6, DATE '2026-08-04'),
    ('Bonsai Ligustrum Sinense In Ceramic', 15.0, 25.0, 'saby-1125', 2, DATE '2026-01-06'),
    ('Bonsai Ficus Retusa In Ceramic',       15.0, 30.0, 'saby-1140', 5, DATE '2026-05-19'),
    ('Bonsai Zelkova Parvifolia In Ceramic', 15.0, 30.0, 'saby-1117', 7, DATE '2026-08-04'),
    ('Bonsai Zelkova Parvifolia In Ceramic', 15.0, 25.0, 'saby-1117', 4, DATE '2026-07-03'),
    ('Bonsai Podocarpus Chinensis In Ceramic', 15.0, 30.0, 'saby-1131', 5, DATE '2026-01-06'),
    ('Sansev Cy Spaghetti',                    6.0, 18.0, 'saby-1173', 11, DATE '2026-05-19'),
    ('Aglao Silver Bay Maria Christina',      11.0, 30.0, 'saby-3234', 1, DATE '2026-07-03'),
    ('Nepent Mix Hang',                       15.0, 30.0, 'saby-3019', 1, DATE '2026-05-19')
)
INSERT INTO procurement_supplier_aliases (
  supplier_id, raw_name, normalized_name, supplier_article,
  pot_diameter_cm, height_cm, matched_saby_id, match_status,
  confidence, availability_status, occurrences, last_seen_at
)
SELECT supplier.id, aliases.raw_name, LOWER(aliases.raw_name), '',
  aliases.pot_diameter_cm, aliases.height_cm, aliases.saby_id, 'confirmed',
  1, 'available', aliases.occurrences, aliases.last_seen_at
FROM supplier CROSS JOIN aliases
JOIN saby_nomenclature nomenclature ON nomenclature.saby_id = aliases.saby_id
ON CONFLICT DO NOTHING;

-- If a line was already learned from an uploaded document, turn that existing
-- line into the verified mapping instead of creating a competing alias.
WITH supplier AS (
  SELECT id FROM procurement_suppliers
  WHERE LOWER(name) = LOWER('Gale ProWay')
  ORDER BY id LIMIT 1
), aliases(
  raw_name, pot_diameter_cm, height_cm, saby_id, occurrences, last_seen_at
) AS (
  VALUES
    ('Bonsai Ligustrum Sinense In Ceramic', 15.0, 30.0, 'saby-1125', 6, DATE '2026-08-04'),
    ('Bonsai Ligustrum Sinense In Ceramic', 15.0, 25.0, 'saby-1125', 2, DATE '2026-01-06'),
    ('Bonsai Ficus Retusa In Ceramic',       15.0, 30.0, 'saby-1140', 5, DATE '2026-05-19'),
    ('Bonsai Zelkova Parvifolia In Ceramic', 15.0, 30.0, 'saby-1117', 7, DATE '2026-08-04'),
    ('Bonsai Zelkova Parvifolia In Ceramic', 15.0, 25.0, 'saby-1117', 4, DATE '2026-07-03'),
    ('Bonsai Podocarpus Chinensis In Ceramic', 15.0, 30.0, 'saby-1131', 5, DATE '2026-01-06'),
    ('Sansev Cy Spaghetti',                    6.0, 18.0, 'saby-1173', 11, DATE '2026-05-19'),
    ('Aglao Silver Bay Maria Christina',      11.0, 30.0, 'saby-3234', 1, DATE '2026-07-03'),
    ('Nepent Mix Hang',                       15.0, 30.0, 'saby-3019', 1, DATE '2026-05-19')
)
UPDATE procurement_supplier_aliases existing SET
  normalized_name = LOWER(aliases.raw_name),
  matched_saby_id = aliases.saby_id,
  match_status = 'confirmed', confidence = 1,
  availability_status = 'available',
  occurrences = GREATEST(existing.occurrences, aliases.occurrences),
  last_seen_at = GREATEST(existing.last_seen_at, aliases.last_seen_at),
  updated_at = CURRENT_TIMESTAMP
FROM supplier, aliases
WHERE existing.supplier_id = supplier.id
  AND LOWER(existing.raw_name) = LOWER(aliases.raw_name)
  AND COALESCE(existing.supplier_article, '') = ''
  AND COALESCE(existing.pot_diameter_cm, -1) = aliases.pot_diameter_cm
  AND COALESCE(existing.height_cm, -1) = aliases.height_cm;

-- These are known WB seller codes from the current marketplace catalogue.
-- They are not nmID values and therefore are stored in the correct field.
WITH channels(saby_id, wb_vendor_code) AS (
  VALUES
    ('saby-1173', 'X341825275'),
    ('saby-3234', 'X9446085'),
    ('saby-3019', 'X9287525')
)
INSERT INTO procurement_product_channels (saby_id, wb_vendor_code)
SELECT channels.saby_id, channels.wb_vendor_code
FROM channels
JOIN saby_nomenclature nomenclature ON nomenclature.saby_id = channels.saby_id
ON CONFLICT (saby_id) DO UPDATE SET
  wb_vendor_code = CASE
    WHEN procurement_product_channels.wb_vendor_code = '' THEN EXCLUDED.wb_vendor_code
    ELSE procurement_product_channels.wb_vendor_code
  END,
  updated_at = CURRENT_TIMESTAMP;
