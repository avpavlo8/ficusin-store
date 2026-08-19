-- Логистика маркетплейса в цене товара.
--
-- В исходной книге «04.08.26.xlsx» цена для маркетплейсов складывалась из
-- розницы, упаковки и логистики: столбец Y листа «закупка» считал её как
-- высоту растения, умноженную на десять рублей за сантиметр. При переносе
-- формулы в код это слагаемое потерялось, и магазин продавал на WB и Ozon
-- дешевле собственного расчёта: для тридцатисантиметрового растения — на
-- 552 рубля с каждой продажи.
--
-- Ставка вынесена в настройку, а не вбита в код: тарифы площадок меняются
-- чаще, чем выходят выкладки. Ноль выключает слагаемое и возвращает
-- прежнее поведение.
ALTER TABLE procurement_pricing_settings
  ADD COLUMN IF NOT EXISTS marketplace_logistics_per_cm NUMERIC(10,2) NOT NULL DEFAULT 10
    CHECK (marketplace_logistics_per_cm >= 0);

UPDATE procurement_pricing_settings SET
  version = version + 1,
  marketplace_logistics_per_cm = 10,
  updated_at = CURRENT_TIMESTAMP
WHERE id = 1;
