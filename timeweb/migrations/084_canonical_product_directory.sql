-- Единый товарный справочник Ficusin.
--
-- Карточка появляется в СБИС, а её видимый бизнес-ключ — код вида X....
-- Внутри базы связи продолжают использовать устойчивый variant_id: внешний
-- сервис не должен становиться физическим FK всего магазина. WB/Ozon IDs,
-- старые артикулы и штрихкоды являются множественными идентификаторами SKU.

ALTER TABLE product_external_ids
  DROP CONSTRAINT IF EXISTS product_external_ids_product_id_variant_id_provider_id_type_key;
DROP INDEX IF EXISTS product_external_ids_product_level_uidx;

ALTER TABLE product_external_ids
  ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active',
  ADD COLUMN IF NOT EXISTS is_primary BOOLEAN NOT NULL DEFAULT TRUE,
  ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'sync',
  ADD COLUMN IF NOT EXISTS linked_by BIGINT REFERENCES customers(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS first_seen_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  ADD COLUMN IF NOT EXISTS confirmed_at TIMESTAMPTZ;

ALTER TABLE product_external_ids
  DROP CONSTRAINT IF EXISTS product_external_ids_status_check;
ALTER TABLE product_external_ids ADD CONSTRAINT product_external_ids_status_check
  CHECK (status IN ('active', 'legacy', 'disabled')) NOT VALID;
ALTER TABLE product_external_ids VALIDATE CONSTRAINT product_external_ids_status_check;

-- У одного SKU может быть несколько действующих и исторических кодов одного
-- типа, но основной в интерфейсе только один. Сам внешний ID по-прежнему
-- глобально принадлежит ровно одному SKU благодаря старому UNIQUE
-- (provider,id_type,external_id).
CREATE UNIQUE INDEX IF NOT EXISTS product_external_ids_primary_variant_uidx
  ON product_external_ids(variant_id, provider, id_type)
  WHERE variant_id IS NOT NULL AND status = 'active' AND is_primary;
CREATE UNIQUE INDEX IF NOT EXISTS product_external_ids_primary_product_uidx
  ON product_external_ids(product_id, provider, id_type)
  WHERE variant_id IS NULL AND status = 'active' AND is_primary;
CREATE INDEX IF NOT EXISTS product_external_ids_resolution_idx
  ON product_external_ids(provider, id_type, external_id, status, variant_id);

-- Продажи и закупка переходят на наш SKU добавочно. Старые saby_id остаются
-- на один совместимый релиз: предыдущий контейнер продолжает обслуживать
-- запросы, пока Timeweb запускает новый.
ALTER TABLE procurement_sales_daily
  ADD COLUMN IF NOT EXISTS canonical_variant_id BIGINT REFERENCES product_variants(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS external_mapping_id BIGINT REFERENCES product_external_ids(id) ON DELETE SET NULL;
ALTER TABLE procurement_supplier_aliases
  ADD COLUMN IF NOT EXISTS canonical_variant_id BIGINT REFERENCES product_variants(id) ON DELETE SET NULL;
ALTER TABLE procurement_order_lines
  ADD COLUMN IF NOT EXISTS canonical_variant_id BIGINT REFERENCES product_variants(id) ON DELETE SET NULL;
ALTER TABLE procurement_requests
  ADD COLUMN IF NOT EXISTS canonical_variant_id BIGINT REFERENCES product_variants(id) ON DELETE SET NULL;
ALTER TABLE procurement_supplier_products
  ADD COLUMN IF NOT EXISTS canonical_variant_id BIGINT REFERENCES product_variants(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS procurement_sales_daily_variant_idx
  ON procurement_sales_daily(canonical_variant_id, sale_date, channel);
CREATE INDEX IF NOT EXISTS procurement_supplier_aliases_variant_idx
  ON procurement_supplier_aliases(canonical_variant_id, supplier_id);

-- Восстанавливаем ручные решения из неизменяемого аудита. Так возвращаются
-- все Ozon offer_id, которые прежняя колонка «один ID на товар» успела
-- перетереть. Для WB внешний код продажи — nmID; рядом сохраняем читаемый
-- артикул продавца из локального зеркала, и именно его покажет интерфейс.
WITH latest AS (
  SELECT DISTINCT ON (after_data->>'channel', after_data->>'externalId')
    after_data->>'channel' AS channel,
    after_data->>'externalId' AS external_id,
    after_data->>'sabyId' AS saby_id,
    actor_customer_id,
    created_at
  FROM admin_audit_log
  WHERE action = 'procurement.sales.link'
    AND after_data->>'channel' IN ('wb', 'ozon')
    AND COALESCE(after_data->>'externalId', '') <> ''
    AND COALESCE(after_data->>'sabyId', '') <> ''
  ORDER BY after_data->>'channel', after_data->>'externalId', created_at DESC, id DESC
), resolved AS (
  SELECT latest.*,
    CASE WHEN latest.channel = 'wb' THEN 'wildberries' ELSE 'ozon' END AS provider,
    CASE WHEN latest.channel = 'wb' THEN 'sku' ELSE 'offer_id' END AS id_type,
    target.product_id, target.variant_id
  FROM latest
  JOIN LATERAL (
    SELECT variant.product_id, variant.id AS variant_id
    FROM product_variants variant
    JOIN products product ON product.id = variant.product_id
    WHERE variant.saby_id = latest.saby_id OR product.saby_id = latest.saby_id
    ORDER BY (variant.saby_id = latest.saby_id) DESC,
      variant.is_active DESC, variant.id
    LIMIT 1
  ) target ON TRUE
)
INSERT INTO product_external_ids(
  product_id, variant_id, provider, id_type, external_id,
  status, is_primary, source, linked_by, confirmed_at, last_seen_at
)
SELECT product_id, variant_id, provider, id_type, external_id,
  'active', FALSE, 'manual', actor_customer_id, created_at, created_at
FROM resolved
ON CONFLICT(provider, id_type, external_id) DO UPDATE SET
  product_id = EXCLUDED.product_id,
  variant_id = EXCLUDED.variant_id,
  status = 'active', is_primary = FALSE,
  source = 'manual',
  linked_by = EXCLUDED.linked_by,
  confirmed_at = EXCLUDED.confirmed_at,
  last_seen_at = GREATEST(product_external_ids.last_seen_at, EXCLUDED.last_seen_at),
  updated_at = CURRENT_TIMESTAMP;

WITH recovered_articles AS (
  SELECT DISTINCT external.product_id, external.variant_id,
    BTRIM(card.article) AS article,
    external.linked_by, external.confirmed_at
  FROM product_external_ids external
  JOIN procurement_channel_products card
    ON card.channel = 'wb' AND card.external_id = external.external_id
  WHERE external.provider = 'wildberries' AND external.id_type = 'sku'
    AND BTRIM(card.article) <> ''
)
INSERT INTO product_external_ids(
  product_id, variant_id, provider, id_type, external_id,
  status, is_primary, source, linked_by, confirmed_at
)
SELECT product_id, variant_id, 'wildberries', 'vendor_code', article,
  'active', FALSE, 'manual', linked_by, confirmed_at
FROM recovered_articles
ON CONFLICT(provider, id_type, external_id) DO UPDATE SET
  product_id = EXCLUDED.product_id,
  variant_id = EXCLUDED.variant_id,
  status = 'active', is_primary = FALSE, source = 'manual',
  linked_by = EXCLUDED.linked_by,
  confirmed_at = EXCLUDED.confirmed_at,
  updated_at = CURRENT_TIMESTAMP;

-- Одноколоночный старый справочник содержит последний рабочий код. Все
-- восстановленные из аудита предыдущие значения остаются разрешаемыми для
-- истории продаж, но не получают новые команды изменения цены.
UPDATE product_external_ids external SET status='legacy',is_primary=FALSE,
  updated_at=CURRENT_TIMESTAMP
FROM procurement_product_channels channel_map
JOIN products product ON product.saby_id=channel_map.saby_id
WHERE external.product_id=product.id AND external.source='manual'
  AND (
    (external.provider='ozon' AND external.id_type='offer_id'
      AND NULLIF(BTRIM(channel_map.ozon_offer_id),'') IS NOT NULL
      AND external.external_id<>BTRIM(channel_map.ozon_offer_id))
    OR (external.provider='wildberries' AND external.id_type='sku'
      AND channel_map.wb_nm_id IS NOT NULL
      AND external.external_id<>channel_map.wb_nm_id::TEXT)
    OR (external.provider='wildberries' AND external.id_type='vendor_code'
      AND NULLIF(BTRIM(channel_map.wb_vendor_code),'') IS NOT NULL
      AND external.external_id<>BTRIM(channel_map.wb_vendor_code))
  );

-- Канонический SKU для всех существующих закупочных связей.
WITH mapped AS (
  SELECT alias.id, target.variant_id
  FROM procurement_supplier_aliases alias
  JOIN LATERAL (
    SELECT variant.id AS variant_id
    FROM product_variants variant
    JOIN products product ON product.id = variant.product_id
    WHERE variant.saby_id = alias.matched_saby_id OR product.saby_id = alias.matched_saby_id
    ORDER BY (variant.saby_id = alias.matched_saby_id) DESC,
      variant.is_active DESC, variant.id
    LIMIT 1
  ) target ON TRUE
  WHERE alias.matched_saby_id IS NOT NULL AND alias.canonical_variant_id IS NULL
)
UPDATE procurement_supplier_aliases alias
SET canonical_variant_id = mapped.variant_id
FROM mapped WHERE alias.id = mapped.id;

WITH mapped AS (
  SELECT line.id, target.variant_id
  FROM procurement_order_lines line
  JOIN LATERAL (
    SELECT variant.id AS variant_id
    FROM product_variants variant
    JOIN products product ON product.id = variant.product_id
    WHERE variant.saby_id = line.saby_id OR product.saby_id = line.saby_id
    ORDER BY (variant.saby_id = line.saby_id) DESC,
      variant.is_active DESC, variant.id
    LIMIT 1
  ) target ON TRUE
  WHERE line.saby_id IS NOT NULL AND line.canonical_variant_id IS NULL
)
UPDATE procurement_order_lines line
SET canonical_variant_id = mapped.variant_id
FROM mapped WHERE line.id = mapped.id;

WITH mapped AS (
  SELECT request.id, target.variant_id
  FROM procurement_requests request
  JOIN LATERAL (
    SELECT variant.id AS variant_id
    FROM product_variants variant
    JOIN products product ON product.id = variant.product_id
    WHERE variant.saby_id = request.saby_id OR product.saby_id = request.saby_id
    ORDER BY (variant.saby_id = request.saby_id) DESC,
      variant.is_active DESC, variant.id
    LIMIT 1
  ) target ON TRUE
  WHERE request.saby_id IS NOT NULL AND request.canonical_variant_id IS NULL
)
UPDATE procurement_requests request
SET canonical_variant_id = mapped.variant_id
FROM mapped WHERE request.id = mapped.id;

WITH mapped AS (
  SELECT supplier_product.supplier_id, supplier_product.saby_id, target.variant_id
  FROM procurement_supplier_products supplier_product
  JOIN LATERAL (
    SELECT variant.id AS variant_id
    FROM product_variants variant
    JOIN products product ON product.id = variant.product_id
    WHERE variant.saby_id = supplier_product.saby_id OR product.saby_id = supplier_product.saby_id
    ORDER BY (variant.saby_id = supplier_product.saby_id) DESC,
      variant.is_active DESC, variant.id
    LIMIT 1
  ) target ON TRUE
  WHERE supplier_product.canonical_variant_id IS NULL
)
UPDATE procurement_supplier_products supplier_product
SET canonical_variant_id = mapped.variant_id
FROM mapped
WHERE supplier_product.supplier_id = mapped.supplier_id
  AND supplier_product.saby_id = mapped.saby_id;

-- Сначала прямые каналы СБИС/сайта, затем WB/Ozon через любое active/legacy
-- соответствие. Legacy участвует в истории продаж намеренно.
WITH mapped AS (
  SELECT sale.channel, sale.sale_date, sale.external_product_id, target.variant_id
  FROM procurement_sales_daily sale
  JOIN LATERAL (
    SELECT variant.id AS variant_id
    FROM product_variants variant
    JOIN products product ON product.id = variant.product_id
    WHERE variant.saby_id = sale.saby_id OR product.saby_id = sale.saby_id
    ORDER BY (variant.saby_id = sale.saby_id) DESC,
      variant.is_active DESC, variant.id
    LIMIT 1
  ) target ON TRUE
  WHERE sale.saby_id IS NOT NULL AND sale.canonical_variant_id IS NULL
)
UPDATE procurement_sales_daily sale
SET canonical_variant_id = mapped.variant_id
FROM mapped
WHERE sale.channel = mapped.channel AND sale.sale_date = mapped.sale_date
  AND sale.external_product_id = mapped.external_product_id;

WITH mapped AS (
  SELECT sale.channel, sale.sale_date, sale.external_product_id,
    mapping.variant_id, mapping.id AS mapping_id
  FROM procurement_sales_daily sale
  JOIN LATERAL (
    SELECT external.id, external.variant_id
    FROM product_external_ids external
    WHERE external.variant_id IS NOT NULL
      AND external.status IN ('active', 'legacy')
      AND external.external_id = sale.external_product_id
      AND (
        (sale.channel = 'wb' AND external.provider = 'wildberries'
          AND external.id_type IN ('sku', 'nm_id'))
        OR (sale.channel = 'ozon' AND external.provider = 'ozon'
          AND external.id_type = 'offer_id')
      )
    ORDER BY (external.source = 'manual') DESC,
      (external.status = 'active') DESC, external.updated_at DESC, external.id DESC
    LIMIT 1
  ) mapping ON TRUE
  WHERE sale.channel IN ('wb', 'ozon')
)
UPDATE procurement_sales_daily sale
SET canonical_variant_id = mapped.variant_id,
  external_mapping_id = mapped.mapping_id
FROM mapped
WHERE sale.channel = mapped.channel AND sale.sale_date = mapped.sale_date
  AND sale.external_product_id = mapped.external_product_id;

-- Единая плоская проекция для административных экранов и отчётов. Главная
-- колонка — код СБИС X..., WB подписан артикулом продавца; nmID остаётся
-- техническим массивом и не становится названием товара.
CREATE OR REPLACE VIEW canonical_product_directory AS
SELECT
  variant.id AS variant_id,
  variant.product_id,
  variant.sku AS internal_sku,
  variant.sku AS display_sku,
  COALESCE(saby_code.external_id, nomenclature.code, '') AS master_code,
  COALESCE(saby_id.external_id, variant.saby_id, product.saby_id, '') AS saby_id,
  product.name,
  variant.label,
  product.status AS product_status,
  variant.is_active <> 0 AND variant.archived_at IS NULL AS active,
  COALESCE(wb.vendor_codes, ARRAY[]::TEXT[]) AS wb_articles,
  COALESCE(wb.legacy_vendor_codes, ARRAY[]::TEXT[]) AS wb_legacy_articles,
  COALESCE(wb.technical_ids, ARRAY[]::TEXT[]) AS wb_nm_ids,
  COALESCE(ozon.offer_ids, ARRAY[]::TEXT[]) AS ozon_articles,
  COALESCE(ozon.legacy_offer_ids, ARRAY[]::TEXT[]) AS ozon_legacy_articles
FROM product_variants variant
JOIN products product ON product.id = variant.product_id
LEFT JOIN LATERAL (
  SELECT external_id FROM product_external_ids
  WHERE product_id = product.id AND (variant_id = variant.id OR variant_id IS NULL)
    AND provider = 'saby' AND id_type = 'code'
    AND status IN ('active', 'legacy')
  ORDER BY (variant_id = variant.id) DESC, (status = 'active') DESC,
    is_primary DESC, updated_at DESC LIMIT 1
) saby_code ON TRUE
LEFT JOIN LATERAL (
  SELECT external_id FROM product_external_ids
  WHERE product_id = product.id AND (variant_id = variant.id OR variant_id IS NULL)
    AND provider = 'saby' AND id_type = 'id'
    AND status IN ('active', 'legacy')
  ORDER BY (variant_id = variant.id) DESC, (status = 'active') DESC,
    is_primary DESC, updated_at DESC LIMIT 1
) saby_id ON TRUE
LEFT JOIN saby_nomenclature nomenclature
  ON nomenclature.saby_id = COALESCE(saby_id.external_id, variant.saby_id, product.saby_id)
LEFT JOIN LATERAL (
  SELECT
    ARRAY_AGG(external_id ORDER BY is_primary DESC, (status = 'active') DESC, updated_at DESC)
      FILTER (WHERE id_type = 'vendor_code' AND status='active') AS vendor_codes,
    ARRAY_AGG(external_id ORDER BY updated_at DESC)
      FILTER (WHERE id_type = 'vendor_code' AND status='legacy') AS legacy_vendor_codes,
    ARRAY_AGG(external_id ORDER BY is_primary DESC, (status = 'active') DESC, updated_at DESC)
      FILTER (WHERE id_type IN ('sku', 'nm_id') AND status='active') AS technical_ids
  FROM product_external_ids
  WHERE variant_id = variant.id AND provider = 'wildberries'
    AND status IN ('active', 'legacy')
) wb ON TRUE
LEFT JOIN LATERAL (
  SELECT ARRAY_AGG(external_id ORDER BY is_primary DESC,updated_at DESC)
      FILTER(WHERE status='active') AS offer_ids,
    ARRAY_AGG(external_id ORDER BY updated_at DESC)
      FILTER(WHERE status='legacy') AS legacy_offer_ids
  FROM product_external_ids
  WHERE variant_id = variant.id AND provider = 'ozon'
    AND id_type = 'offer_id' AND status IN ('active', 'legacy')
) ozon ON TRUE;

-- Совместимый мост для старых записей procurement_product_channels. Он
-- больше ничего не удаляет: сменившийся ID переводится в legacy, новый
-- становится основным. Человеческий WB-ключ — vendor_code; nmID хранится
-- только как технический `sku` старого API-контракта.
CREATE OR REPLACE FUNCTION sync_marketplace_external_ids() RETURNS trigger AS $$
DECLARE mapped_product BIGINT; mapped_variant BIGINT;
BEGIN
  SELECT p.id, pv.id INTO mapped_product, mapped_variant
  FROM products p
  LEFT JOIN LATERAL (
    SELECT id FROM product_variants
    WHERE product_id = p.id ORDER BY is_active DESC, id LIMIT 1
  ) pv ON TRUE
  WHERE p.saby_id = NEW.saby_id;
  IF mapped_product IS NULL OR mapped_variant IS NULL THEN RETURN NEW; END IF;

  IF NEW.wb_nm_id IS NOT NULL THEN
    UPDATE product_external_ids SET is_primary = FALSE,
      status = CASE WHEN external_id = NEW.wb_nm_id::TEXT THEN 'active' ELSE 'legacy' END,
      updated_at = CURRENT_TIMESTAMP
    WHERE variant_id = mapped_variant AND provider = 'wildberries' AND id_type = 'sku';
    INSERT INTO product_external_ids(product_id, variant_id, provider, id_type,
      external_id, status, is_primary, source, last_seen_at)
    VALUES(mapped_product, mapped_variant, 'wildberries', 'sku',
      NEW.wb_nm_id::TEXT, 'active', TRUE, 'sync', CURRENT_TIMESTAMP)
    ON CONFLICT(provider, id_type, external_id) DO UPDATE SET
      product_id = EXCLUDED.product_id, variant_id = EXCLUDED.variant_id,
      status = 'active', is_primary = TRUE, last_seen_at = CURRENT_TIMESTAMP,
      updated_at = CURRENT_TIMESTAMP;
  END IF;
  IF NULLIF(BTRIM(NEW.wb_vendor_code), '') IS NOT NULL THEN
    UPDATE product_external_ids SET is_primary = FALSE, status = 'legacy', updated_at = CURRENT_TIMESTAMP
    WHERE variant_id = mapped_variant AND provider = 'wildberries'
      AND id_type = 'vendor_code';
    INSERT INTO product_external_ids(product_id, variant_id, provider, id_type,
      external_id, status, is_primary, source, last_seen_at)
    VALUES(mapped_product, mapped_variant, 'wildberries', 'vendor_code',
      BTRIM(NEW.wb_vendor_code), 'active', TRUE, 'sync', CURRENT_TIMESTAMP)
    ON CONFLICT(provider, id_type, external_id) DO UPDATE SET
      product_id = EXCLUDED.product_id, variant_id = EXCLUDED.variant_id,
      status = 'active', is_primary = TRUE, last_seen_at = CURRENT_TIMESTAMP,
      updated_at = CURRENT_TIMESTAMP;
  END IF;
  IF NULLIF(BTRIM(NEW.ozon_offer_id), '') IS NOT NULL THEN
    UPDATE product_external_ids SET is_primary = FALSE, status = 'legacy', updated_at = CURRENT_TIMESTAMP
    WHERE variant_id = mapped_variant AND provider = 'ozon' AND id_type = 'offer_id';
    INSERT INTO product_external_ids(product_id, variant_id, provider, id_type,
      external_id, status, is_primary, source, last_seen_at)
    VALUES(mapped_product, mapped_variant, 'ozon', 'offer_id',
      BTRIM(NEW.ozon_offer_id), 'active', TRUE, 'sync', CURRENT_TIMESTAMP)
    ON CONFLICT(provider, id_type, external_id) DO UPDATE SET
      product_id = EXCLUDED.product_id, variant_id = EXCLUDED.variant_id,
      status = 'active', is_primary = TRUE, last_seen_at = CURRENT_TIMESTAMP,
      updated_at = CURRENT_TIMESTAMP;
  END IF;
  RETURN NEW;
END $$ LANGUAGE plpgsql;
