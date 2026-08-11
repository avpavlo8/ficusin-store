-- Базовая версия формулы перенесена из книги «04.08.26.xlsx».
-- Финансовые параметры соответствуют листу «Настройки», а коэффициент
-- цены без скидки (+20%) — формуле на листе «Цена для МП».
UPDATE procurement_pricing_settings SET
  version = version + 1,
  default_exchange_rate = 120,
  trolley_cost_currency = 0,
  trolley_cost_rub = 63700,
  trolley_volume_cm3 = 1965600,
  trolley_fill_ratio = 0.55,
  return_loss_rate = 0.05,
  marketplace_cost_rate = 0.46,
  tax_rate = 0.08,
  reserve_rate = 0.25,
  package_rub = 150,
  price_change_threshold = 0.10,
  domestic_retail_multiplier = 2.1,
  international_cost_multiplier = 2.0,
  international_retail_multiplier = 1.1,
  marketplace_strike_markup = 0.20,
  retail_round_step = 50,
  avoid_round_hundreds = TRUE,
  recommendation_days = 60,
  target_cover_days = 45,
  updated_at = CURRENT_TIMESTAMP
WHERE id = 1;
