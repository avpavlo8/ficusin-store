-- Ручной разбор продаж маркетплейсов.
--
-- Автосвязывание идёт по точному совпадению кода, артикула или штрихкода;
-- совпадение по названию не используется, потому что «Фикус Бенджамина 12»
-- и «Фикус Бенджамина 14» — разные растения с разной ценой. Всё, что не
-- совпало, остаётся в продажах без saby_id и в расчёт закупки не попадает:
-- у Ozon так висит около шестисот строк. Разбирать их придётся руками.

-- Список несопоставленного ищется по каналу среди пустых saby_id, а
-- существующий индекс построен по saby_id и такой поиск не обслуживает:
-- искать нужно как раз там, где значения нет.
CREATE INDEX IF NOT EXISTS procurement_sales_daily_unlinked_idx
  ON procurement_sales_daily (channel, external_product_id)
  WHERE saby_id IS NULL;

-- Разовая засыпка по уже проставленным связям.
--
-- saby_id пишется только в момент вставки продажи, поэтому карточка,
-- связанная кнопкой «Подтянуть артикулы» уже после загрузки продаж, чинила
-- только будущие строки. Прошлые ждали бы следующей глубокой выгрузки за
-- год, а строки старше её окна не починились бы никогда.
UPDATE procurement_sales_daily sale
SET saby_id = channels.saby_id
FROM procurement_product_channels channels
WHERE sale.saby_id IS NULL
  AND (
    (sale.channel = 'ozon'
      AND channels.ozon_offer_id <> ''
      AND channels.ozon_offer_id = sale.external_product_id)
    OR (sale.channel = 'wb'
      AND channels.wb_nm_id IS NOT NULL
      AND channels.wb_nm_id::TEXT = sale.external_product_id)
  );
