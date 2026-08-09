-- Карточки товаров становятся нашими.
--
-- Раньше обмен с СБИС перезаписывал у товара всё подряд и убирал с витрины
-- всё, чего не оказалось в выгрузке. Теперь наоборот: у каждого товара
-- перечислено, что именно разрешено брать из СБИС, и по умолчанию это
-- только остаток. Название, описание, цену и фотографии магазин ведёт сам,
-- а подтянуть их из СБИС можно по кнопке — осознанно и по одному полю.
ALTER TABLE products
  ADD COLUMN IF NOT EXISTS saby_fields TEXT[] NOT NULL DEFAULT ARRAY['stock']::TEXT[];

-- У товаров, заведённых до этой перемены, остаётся тот же единственный
-- источник — остаток. У наших собственных карточек из СБИС не берётся
-- ничего: связи с ним у них попросту нет.
UPDATE products SET saby_fields = ARRAY['stock']::TEXT[] WHERE saby_id IS NOT NULL;
UPDATE products SET saby_fields = ARRAY[]::TEXT[] WHERE saby_id IS NULL;

-- Справочник номенклатуры СБИС.
--
-- Обмен складывает сюда всё, что видит, включая то, чего на витрине нет.
-- Из этого справочника менеджер выбирает, что завести в магазин: импорт по
-- кодам ищет здесь, а не ходит в СБИС в момент нажатия кнопки. Заодно это
-- развязывает магазин: даже если СБИС недоступен, справочник на месте.
CREATE TABLE IF NOT EXISTS saby_nomenclature (
  saby_id TEXT PRIMARY KEY,
  -- Код товара — то, что менеджер видит в СБИС и вставляет при импорте
  -- (вида X1150532). В выгрузке он зовётся по-разному, поэтому рядом лежат
  -- артикул и штрихкод: искать будем по любому из трёх.
  code TEXT NOT NULL DEFAULT '',
  article TEXT NOT NULL DEFAULT '',
  barcode TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  price_minor BIGINT NOT NULL DEFAULT 0,
  balance INTEGER NOT NULL DEFAULT 0,
  images TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
  seen_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  -- Заполняется, когда позиция пропала из выгрузки. Карточку на сайте это
  -- не трогает — только обнуляет остаток.
  missing_since TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS saby_nomenclature_code_idx ON saby_nomenclature (code);
CREATE INDEX IF NOT EXISTS saby_nomenclature_article_idx ON saby_nomenclature (article);
CREATE INDEX IF NOT EXISTS saby_nomenclature_barcode_idx ON saby_nomenclature (barcode);
