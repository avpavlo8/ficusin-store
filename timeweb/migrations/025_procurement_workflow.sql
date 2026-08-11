-- Полный рабочий контур закупки: версия формулы, рассчитанная себестоимость,
-- предложения цен и безопасная очередь внешних действий. Подтверждение
-- никогда не теряет историю и не выдаёт неотправленное изменение за успех.
CREATE TABLE IF NOT EXISTS procurement_pricing_settings (
  id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
  version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
  default_exchange_rate NUMERIC(14,6) NOT NULL DEFAULT 1 CHECK (default_exchange_rate > 0),
  trolley_cost_currency NUMERIC(14,2) NOT NULL DEFAULT 0 CHECK (trolley_cost_currency >= 0),
  trolley_volume_cm3 NUMERIC(14,2) NOT NULL DEFAULT 1 CHECK (trolley_volume_cm3 > 0),
  trolley_fill_ratio NUMERIC(7,6) NOT NULL DEFAULT 1 CHECK (trolley_fill_ratio > 0 AND trolley_fill_ratio <= 1),
  return_loss_rate NUMERIC(7,6) NOT NULL DEFAULT 0 CHECK (return_loss_rate >= 0 AND return_loss_rate < 1),
  marketplace_cost_rate NUMERIC(7,6) NOT NULL DEFAULT 0 CHECK (marketplace_cost_rate >= 0 AND marketplace_cost_rate < 1),
  tax_rate NUMERIC(7,6) NOT NULL DEFAULT 0 CHECK (tax_rate >= 0 AND tax_rate < 1),
  reserve_rate NUMERIC(7,6) NOT NULL DEFAULT 0 CHECK (reserve_rate >= 0 AND reserve_rate < 1),
  package_rub NUMERIC(14,2) NOT NULL DEFAULT 0 CHECK (package_rub >= 0),
  price_change_threshold NUMERIC(7,6) NOT NULL DEFAULT 0.10 CHECK (price_change_threshold >= 0 AND price_change_threshold < 1),
  domestic_retail_multiplier NUMERIC(8,4) NOT NULL DEFAULT 1 CHECK (domestic_retail_multiplier > 0),
  international_cost_multiplier NUMERIC(8,4) NOT NULL DEFAULT 1 CHECK (international_cost_multiplier > 0),
  international_retail_multiplier NUMERIC(8,4) NOT NULL DEFAULT 1 CHECK (international_retail_multiplier > 0),
  marketplace_strike_markup NUMERIC(7,6) NOT NULL DEFAULT 0 CHECK (marketplace_strike_markup >= 0 AND marketplace_strike_markup < 1),
  retail_round_step INTEGER NOT NULL DEFAULT 1 CHECK (retail_round_step > 0),
  avoid_round_hundreds BOOLEAN NOT NULL DEFAULT FALSE,
  recommendation_days INTEGER NOT NULL DEFAULT 30 CHECK (recommendation_days BETWEEN 7 AND 365),
  target_cover_days INTEGER NOT NULL DEFAULT 30 CHECK (target_cover_days BETWEEN 1 AND 365),
  updated_by BIGINT REFERENCES customers(id) ON DELETE SET NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO procurement_pricing_settings (id) VALUES (1) ON CONFLICT (id) DO NOTHING;

ALTER TABLE procurement_orders
  ADD COLUMN IF NOT EXISTS calculation_version INTEGER,
  ADD COLUMN IF NOT EXISTS calculation_settings JSONB,
  ADD COLUMN IF NOT EXISTS calculated_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS approved_at TIMESTAMPTZ;

ALTER TABLE procurement_order_lines
  ADD COLUMN IF NOT EXISTS expected_unit_price NUMERIC(14,4),
  ADD COLUMN IF NOT EXISTS purchase_unit_rub NUMERIC(14,2),
  ADD COLUMN IF NOT EXISTS trolley_delivery_unit_rub NUMERIC(14,2),
  ADD COLUMN IF NOT EXISTS ryazan_delivery_unit_rub NUMERIC(14,2),
  ADD COLUMN IF NOT EXISTS unit_cost_rub NUMERIC(14,2),
  ADD COLUMN IF NOT EXISTS proposed_retail_rub BIGINT,
  ADD COLUMN IF NOT EXISTS proposed_marketplace_rub BIGINT,
  ADD COLUMN IF NOT EXISTS proposed_marketplace_strike_rub BIGINT;

-- Канальные артикулы относятся к каноническому товару, а варианты названий
-- поставщика продолжают жить в procurement_supplier_aliases.
CREATE TABLE IF NOT EXISTS procurement_product_channels (
  saby_id TEXT PRIMARY KEY REFERENCES saby_nomenclature(saby_id) ON DELETE CASCADE,
  holland_article TEXT NOT NULL DEFAULT '',
  wb_article TEXT NOT NULL DEFAULT '',
  ozon_article TEXT NOT NULL DEFAULT '',
  updated_by BIGINT REFERENCES customers(id) ON DELETE SET NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS procurement_product_channels_holland_idx
  ON procurement_product_channels (holland_article) WHERE holland_article <> '';

CREATE TABLE IF NOT EXISTS procurement_action_batches (
  id BIGSERIAL PRIMARY KEY,
  procurement_order_id BIGINT NOT NULL REFERENCES procurement_orders(id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK (kind IN ('receipt', 'prices')),
  status TEXT NOT NULL DEFAULT 'draft'
    CHECK (status IN ('draft', 'approved', 'processing', 'partially_completed', 'completed', 'failed', 'cancelled')),
  created_by BIGINT REFERENCES customers(id) ON DELETE SET NULL,
  approved_by BIGINT REFERENCES customers(id) ON DELETE SET NULL,
  approved_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS procurement_action_batches_active_unique
  ON procurement_action_batches (procurement_order_id, kind)
  WHERE status NOT IN ('cancelled', 'completed');

CREATE TABLE IF NOT EXISTS procurement_action_items (
  id BIGSERIAL PRIMARY KEY,
  batch_id BIGINT NOT NULL REFERENCES procurement_action_batches(id) ON DELETE CASCADE,
  procurement_order_line_id BIGINT NOT NULL REFERENCES procurement_order_lines(id) ON DELETE CASCADE,
  channel TEXT NOT NULL CHECK (channel IN ('saby_receipt', 'site', 'saby_price', 'wb', 'ozon')),
  external_article TEXT NOT NULL DEFAULT '',
  old_value NUMERIC(14,2),
  new_value NUMERIC(14,2) NOT NULL,
  quantity INTEGER,
  status TEXT NOT NULL DEFAULT 'draft'
    CHECK (status IN ('draft', 'approved', 'queued', 'completed', 'failed', 'not_configured', 'skipped')),
  error_message TEXT NOT NULL DEFAULT '',
  attempts INTEGER NOT NULL DEFAULT 0,
  completed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE (batch_id, procurement_order_line_id, channel)
);
CREATE INDEX IF NOT EXISTS procurement_action_items_status_idx
  ON procurement_action_items (status, channel, created_at);
