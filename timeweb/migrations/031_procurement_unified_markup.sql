-- Одна понятная наценка применяется к полной себестоимости независимо от
-- страны поставщика. Старые коэффициенты остаются только для совместимости
-- с историческими снимками уже рассчитанных закупок.
ALTER TABLE procurement_pricing_settings
  ADD COLUMN IF NOT EXISTS retail_markup_multiplier NUMERIC(8,4) NOT NULL DEFAULT 2.1
    CHECK (retail_markup_multiplier > 0),
  ADD COLUMN IF NOT EXISTS round_prices BOOLEAN NOT NULL DEFAULT TRUE;

UPDATE procurement_pricing_settings SET
  version = version + 1,
  retail_markup_multiplier = 2.1,
  round_prices = TRUE,
  updated_at = CURRENT_TIMESTAMP
WHERE id = 1;
