-- Рекомендации к закупке: один источник правды о наличии и явный список
-- «не закупаем».
--
-- До этой миграции статус наличия жил в двух местах сразу — на алиасе
-- (написание из инвойса) и на паре поставщик+товар. Рекомендации читали
-- пару, очередь проверки читала алиас, а кнопка в интерфейсе была доступна
-- только у товаров, чьё название хоть раз встретилось в разобранном PDF.
-- Товар из справочника пометить было нечем. Теперь единственный источник —
-- procurement_supplier_products, а алиас остаётся тем, чем он и был:
-- словарём написаний.

-- Переносим на пару всё, что успели пометить на алиасе. Правка, сделанная
-- руками в карточке товара, новее любого автоматического статуса, поэтому
-- трогаем только пары, до которых никто не дошёл.
UPDATE procurement_supplier_products product
SET availability_status = alias.availability_status,
  check_after = COALESCE(product.check_after, alias.check_after),
  unavailable_since = COALESCE(product.unavailable_since, alias.unavailable_since),
  updated_at = CURRENT_TIMESTAMP
FROM (
  SELECT DISTINCT ON (supplier_id, matched_saby_id)
    supplier_id, matched_saby_id, availability_status, check_after, unavailable_since
  FROM procurement_supplier_aliases
  WHERE matched_saby_id IS NOT NULL AND availability_status <> 'unknown'
  ORDER BY supplier_id, matched_saby_id, updated_at DESC, id DESC
) alias
WHERE product.supplier_id = alias.supplier_id
  AND product.saby_id = alias.matched_saby_id
  AND product.availability_status = 'unknown';

-- Товар, снятый с закупки решением магазина. Это не про поставщика: у него
-- растение может быть в наличии, просто мы его пока не берём. Поэтому ключ —
-- товар целиком, а не пара с поставщиком.
CREATE TABLE IF NOT EXISTS procurement_excluded_products (
  saby_id TEXT PRIMARY KEY REFERENCES saby_nomenclature(saby_id) ON DELETE CASCADE,
  reason TEXT NOT NULL DEFAULT '',
  excluded_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_by BIGINT REFERENCES customers(id) ON DELETE SET NULL
);
