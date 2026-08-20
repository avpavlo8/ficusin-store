-- В августе 2026 ключ Saby для витрины был переведён с человекочитаемого
-- X-кода на внутренний id catalogue endpoint. Старые строки справочника при
-- этом намеренно не удалялись. Если карточку импортировали в переходный
-- период, она могла привязаться к исторической строке saby_id='X...' с
-- missing_since и навсегда получать из неё нулевой остаток.
--
-- Ремонтируем только однозначные случаи:
--   * текущая связь товара указывает на пропавшую строку X...;
--   * у неё есть ровно одна активная строка с тем же Saby code;
--   * новый id ещё не занят другой карточкой/вариантом/mapping.
-- URL, тексты, фотографии, цены и SKU товара не меняются.

CREATE TEMP TABLE saby_identity_repairs ON COMMIT DROP AS
WITH active_codes AS (
  SELECT UPPER(BTRIM(code)) AS code_key, MIN(saby_id) AS current_saby_id
  FROM saby_nomenclature
  WHERE missing_since IS NULL
    AND NULLIF(BTRIM(code), '') IS NOT NULL
  GROUP BY UPPER(BTRIM(code))
  HAVING COUNT(*) = 1
)
SELECT
  p.id AS product_id,
  pv.id AS variant_id,
  old.saby_id AS old_saby_id,
  current.saby_id AS current_saby_id,
  current.balance AS current_balance
FROM products p
JOIN product_variants pv
  ON pv.product_id = p.id AND pv.saby_id = p.saby_id
JOIN saby_nomenclature old ON old.saby_id = p.saby_id
JOIN active_codes active ON active.code_key = UPPER(BTRIM(old.code))
JOIN saby_nomenclature current ON current.saby_id = active.current_saby_id
WHERE old.missing_since IS NOT NULL
  AND old.saby_id <> current.saby_id
  AND UPPER(BTRIM(old.saby_id)) = UPPER(BTRIM(old.code))
  AND UPPER(BTRIM(old.saby_id)) ~ '^X[0-9]+$'
  AND NOT EXISTS (
    SELECT 1 FROM products other
    WHERE other.saby_id = current.saby_id AND other.id <> p.id
  )
  AND NOT EXISTS (
    SELECT 1 FROM product_variants other
    WHERE other.saby_id = current.saby_id AND other.id <> pv.id
  )
  AND NOT EXISTS (
    SELECT 1 FROM product_external_ids external
    WHERE external.provider = 'saby' AND external.id_type = 'id'
      AND external.external_id = current.saby_id
      AND external.product_id <> p.id
  );

-- Убираем только старое техническое сопоставление id у ремонтируемых
-- карточек. Код X... остаётся внешним code mapping и продолжает быть тем,
-- что менеджер вводит в интерфейсе импорта.
DELETE FROM product_external_ids external
USING saby_identity_repairs repair
WHERE external.product_id = repair.product_id
  AND external.provider = 'saby'
  AND external.id_type = 'id';

UPDATE product_variants variant
SET saby_id = repair.current_saby_id,
    saby_updated_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
FROM saby_identity_repairs repair
WHERE variant.id = repair.variant_id;

UPDATE products product
SET saby_id = repair.current_saby_id,
    saby_updated_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
FROM saby_identity_repairs repair
WHERE product.id = repair.product_id;

INSERT INTO product_external_ids(
  product_id, variant_id, provider, id_type, external_id, updated_at
)
SELECT product_id, variant_id, 'saby', 'id', current_saby_id, CURRENT_TIMESTAMP
FROM saby_identity_repairs
ON CONFLICT(provider, id_type, external_id) DO UPDATE SET
  product_id = EXCLUDED.product_id,
  variant_id = EXCLUDED.variant_id,
  updated_at = CURRENT_TIMESTAMP;

-- Не ждём следующего обмена: если активная строка справочника уже содержит
-- актуальный balance, тот же остаток сразу попадает в существующий inventory.
INSERT INTO warehouses (saby_id, name, city, address, is_active)
VALUES ('saby-ryazan-main', 'Основной склад', 'Рязань', 'Новосёлов, 40А', 1)
ON CONFLICT (saby_id) DO NOTHING;

INSERT INTO inventory(warehouse_id, variant_id, available_qty, reserved_qty, synced_at)
SELECT warehouse.id, repair.variant_id, repair.current_balance, 0, CURRENT_TIMESTAMP
FROM saby_identity_repairs repair
JOIN warehouses warehouse ON warehouse.saby_id = 'saby-ryazan-main'
ON CONFLICT (warehouse_id, variant_id) DO UPDATE SET
  available_qty = EXCLUDED.available_qty,
  synced_at = CURRENT_TIMESTAMP;
