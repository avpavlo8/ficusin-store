CREATE TABLE IF NOT EXISTS procurement_sales_daily (
  channel TEXT NOT NULL CHECK (channel IN ('site', 'saby', 'wb', 'ozon')),
  sale_date DATE NOT NULL,
  external_product_id TEXT NOT NULL,
  saby_id TEXT REFERENCES saby_nomenclature(saby_id) ON DELETE SET NULL,
  units INTEGER NOT NULL,
  gross_rub NUMERIC(14, 2) NOT NULL DEFAULT 0,
  synced_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (channel, sale_date, external_product_id)
);

CREATE INDEX IF NOT EXISTS procurement_sales_daily_saby_idx
  ON procurement_sales_daily (saby_id, sale_date, channel);

CREATE TABLE IF NOT EXISTS procurement_sales_sync_state (
  channel TEXT PRIMARY KEY CHECK (channel IN ('site', 'saby', 'wb', 'ozon')),
  status TEXT NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending', 'running', 'ok', 'error', 'disabled')),
  last_attempt_at TIMESTAMPTZ,
  last_success_at TIMESTAMPTZ,
  last_error TEXT NOT NULL DEFAULT '',
  rows_synced INTEGER NOT NULL DEFAULT 0,
  period_from DATE,
  period_to DATE,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO procurement_sales_sync_state (channel)
VALUES ('site'), ('saby'), ('wb'), ('ozon')
ON CONFLICT (channel) DO NOTHING;

INSERT INTO procurement_sales_daily (
  channel, sale_date, external_product_id, saby_id, units, gross_rub
)
SELECT 'site', o.created_at::DATE, pv.saby_id, pv.saby_id,
  SUM(oi.quantity)::INTEGER,
  SUM(oi.quantity * oi.unit_price)::NUMERIC
FROM orders o
JOIN order_items oi ON oi.order_id = o.id
JOIN product_variants pv ON pv.id = oi.variant_id
WHERE o.status <> 'cancelled' AND pv.saby_id IS NOT NULL
GROUP BY o.created_at::DATE, pv.saby_id
ON CONFLICT (channel, sale_date, external_product_id) DO UPDATE SET
  saby_id = EXCLUDED.saby_id,
  units = EXCLUDED.units,
  gross_rub = EXCLUDED.gross_rub,
  synced_at = CURRENT_TIMESTAMP;

UPDATE procurement_sales_sync_state SET
  status = 'ok', last_attempt_at = CURRENT_TIMESTAMP,
  last_success_at = CURRENT_TIMESTAMP,
  rows_synced = (SELECT COUNT(*) FROM procurement_sales_daily WHERE channel = 'site'),
  period_from = (SELECT MIN(sale_date) FROM procurement_sales_daily WHERE channel = 'site'),
  period_to = CURRENT_DATE, updated_at = CURRENT_TIMESTAMP
WHERE channel = 'site';
