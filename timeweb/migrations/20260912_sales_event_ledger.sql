-- Источник истины для аналитики продаж. Суточная таблица ниже остаётся
-- совместимым агрегатом для расчёта закупок.
CREATE TABLE IF NOT EXISTS sales_events (
  id BIGSERIAL PRIMARY KEY,
  channel TEXT NOT NULL CHECK (channel IN ('site','saby','wb','ozon')),
  source_event_id TEXT NOT NULL,
  source_document_id TEXT NOT NULL DEFAULT '',
  source_line_id TEXT NOT NULL DEFAULT '',
  cross_source_key TEXT NOT NULL DEFAULT '',
  event_type TEXT NOT NULL CHECK (event_type IN ('sale','return','cancellation','correction')),
  event_status TEXT NOT NULL CHECK (event_status IN ('confirmed','pending','cancelled')),
  event_at TIMESTAMPTZ NOT NULL,
  imported_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  external_product_id TEXT NOT NULL,
  saby_id TEXT REFERENCES saby_nomenclature(saby_id) ON DELETE SET NULL,
  canonical_variant_id BIGINT REFERENCES product_variants(id) ON DELETE SET NULL,
  external_mapping_id BIGINT REFERENCES product_external_ids(id) ON DELETE SET NULL,
  units INTEGER NOT NULL CHECK (units >= 0),
  gross_rub NUMERIC(14,2) NOT NULL CHECK (gross_rub >= 0),
  effect SMALLINT NOT NULL DEFAULT 1 CHECK (effect IN (-1,1)),
  reconciliation_status TEXT NOT NULL DEFAULT 'counted'
    CHECK (reconciliation_status IN ('counted','duplicate','unmatched','ambiguous','excluded')),
  duplicate_of BIGINT REFERENCES sales_events(id) ON DELETE SET NULL,
  import_batch_id UUID NOT NULL,
  raw_document JSONB NOT NULL DEFAULT '{}'::JSONB,
  UNIQUE(channel, source_event_id, source_line_id)
);

CREATE INDEX IF NOT EXISTS sales_events_period_idx ON sales_events(event_at, channel);
CREATE INDEX IF NOT EXISTS sales_events_product_idx ON sales_events(canonical_variant_id, event_at);
CREATE INDEX IF NOT EXISTS sales_events_cross_source_idx
  ON sales_events(cross_source_key) WHERE cross_source_key <> '';
CREATE INDEX IF NOT EXISTS sales_events_reconciliation_idx
  ON sales_events(reconciliation_status, event_at);

-- Старые агрегаты сохраняются как явно отмеченное наследие: они доступны
-- для закупок, но не притворяются документами в новой аналитике.
INSERT INTO sales_events(
  channel,source_event_id,source_document_id,source_line_id,event_type,event_status,
  event_at,external_product_id,saby_id,canonical_variant_id,external_mapping_id,
  units,gross_rub,effect,reconciliation_status,import_batch_id,raw_document
)
SELECT channel,
  'legacy:' || sale_date::TEXT || ':' || md5(external_product_id),
  'legacy-daily:' || sale_date::TEXT,external_product_id,
  CASE WHEN units < 0 OR gross_rub < 0 THEN 'return' ELSE 'sale' END,'confirmed',
  sale_date::TIMESTAMP AT TIME ZONE 'Europe/Moscow',external_product_id,saby_id,
  canonical_variant_id,external_mapping_id,ABS(units),ABS(gross_rub),
  CASE WHEN units < 0 OR gross_rub < 0 THEN -1 ELSE 1 END,
  CASE WHEN saby_id IS NULL THEN 'unmatched' ELSE 'counted' END,gen_random_uuid(),
  jsonb_build_object('legacyDaily',true)
FROM procurement_sales_daily
ON CONFLICT(channel,source_event_id,source_line_id) DO NOTHING;
