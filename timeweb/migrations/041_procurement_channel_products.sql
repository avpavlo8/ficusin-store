-- Подписи карточек маркетплейса.
--
-- У Ozon внешний код продажи — offer_id, и он обычно читается как название
-- («кумкват», «мухоловка»). У Wildberries это nmID, то есть голое число:
-- по строке «1851256804» человек не скажет, какое это растение, и разбирать
-- продажи вслепую невозможно.
--
-- Площадка отдаёт название и артикул продавца вместе со справочником
-- карточек, который магазин уже читает кнопкой «Подтянуть артикулы». Раньше
-- эти поля использовались только для сравнения ключей и выбрасывались.
-- Теперь остаются здесь — как подпись к коду, а не как источник правды:
-- связь с товаром по-прежнему живёт в procurement_product_channels.
CREATE TABLE IF NOT EXISTS procurement_channel_products (
  channel TEXT NOT NULL CHECK (channel IN ('wb', 'ozon')),
  external_id TEXT NOT NULL,
  article TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL DEFAULT '',
  seen_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (channel, external_id)
);
