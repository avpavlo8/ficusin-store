-- Stage 13 finance completion. All changes are additive because the previous
-- finance migrations may already be present in an acceptance database.
ALTER TABLE finance_packaging_snapshots
  ADD COLUMN IF NOT EXISTS recognition_date DATE;
UPDATE finance_packaging_snapshots p SET recognition_date=CASE WHEN s.channel IN ('ozon','wb') THEN (s.event_at AT TIME ZONE 'Europe/Moscow')::DATE ELSE (p.created_at AT TIME ZONE 'Europe/Moscow')::DATE END
FROM sales_events s WHERE s.id=p.sales_event_id AND p.recognition_date IS NULL;
ALTER TABLE finance_packaging_snapshots ALTER COLUMN recognition_date SET NOT NULL;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS shipped_at TIMESTAMPTZ;
ALTER TABLE shipment_offers ADD COLUMN IF NOT EXISTS shipped_at TIMESTAMPTZ;

CREATE OR REPLACE FUNCTION stamp_actual_shipment_time() RETURNS TRIGGER AS $$
BEGIN
  IF NEW.status IN ('shipped','completed') AND OLD.status IS DISTINCT FROM NEW.status AND NEW.shipped_at IS NULL THEN
    NEW.shipped_at := CURRENT_TIMESTAMP;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS orders_actual_shipment_time ON orders;
CREATE TRIGGER orders_actual_shipment_time BEFORE UPDATE OF status ON orders FOR EACH ROW EXECUTE FUNCTION stamp_actual_shipment_time();
DROP TRIGGER IF EXISTS shipment_offers_actual_shipment_time ON shipment_offers;
CREATE TRIGGER shipment_offers_actual_shipment_time BEFORE UPDATE OF status ON shipment_offers FOR EACH ROW EXECUTE FUNCTION stamp_actual_shipment_time();

-- A snapshot is a historical expense. Once recognized its rate, quantity and
-- recognition date are immutable; later reimports cannot move it between months.
CREATE OR REPLACE FUNCTION snapshot_finance_packaging() RETURNS TRIGGER AS $$
DECLARE recognized DATE;
BEGIN
  IF finance_packaging_eligible(NEW) THEN
    recognized := CASE WHEN NEW.channel IN ('ozon','wb') THEN (NEW.event_at AT TIME ZONE 'Europe/Moscow')::DATE ELSE COALESCE(
      (SELECT (so.shipped_at AT TIME ZONE 'Europe/Moscow')::DATE FROM shipment_offers so JOIN orders o ON o.id=so.order_id WHERE o.order_number=NEW.source_document_id AND NEW.source_line_id LIKE 'offer:'||so.id::TEXT||':%' LIMIT 1),
      (SELECT (o.shipped_at AT TIME ZONE 'Europe/Moscow')::DATE FROM orders o WHERE o.order_number=NEW.source_document_id),
      (CURRENT_TIMESTAMP AT TIME ZONE 'Europe/Moscow')::DATE) END;
    INSERT INTO finance_packaging_snapshots(sales_event_id,units,rate_rub,amount_rub,recognition_date)
    VALUES(NEW.id,NEW.units,150,NEW.units*150,recognized)
    ON CONFLICT(sales_event_id) DO NOTHING;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION recognize_site_order_packaging() RETURNS TRIGGER AS $$
BEGIN
  IF NEW.status IN ('shipped','completed') AND OLD.status IS DISTINCT FROM NEW.status THEN
    INSERT INTO finance_packaging_snapshots(sales_event_id,units,rate_rub,amount_rub,recognition_date)
    SELECT s.id,s.units,150,s.units*150,(NEW.shipped_at AT TIME ZONE 'Europe/Moscow')::DATE
    FROM sales_events s WHERE s.channel='site' AND s.source_document_id=NEW.order_number
      AND finance_packaging_eligible(s)
    ON CONFLICT(sales_event_id) DO NOTHING;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS orders_finance_packaging_recognition ON orders;
CREATE TRIGGER orders_finance_packaging_recognition AFTER UPDATE OF status ON orders
FOR EACH ROW EXECUTE FUNCTION recognize_site_order_packaging();

CREATE OR REPLACE FUNCTION recognize_site_offer_packaging() RETURNS TRIGGER AS $$
BEGIN
  IF NEW.status IN ('shipped','completed') AND OLD.status IS DISTINCT FROM NEW.status THEN
    INSERT INTO finance_packaging_snapshots(sales_event_id,units,rate_rub,amount_rub,recognition_date)
    SELECT s.id,s.units,150,s.units*150,(NEW.shipped_at AT TIME ZONE 'Europe/Moscow')::DATE
    FROM sales_events s JOIN orders o ON o.id=NEW.order_id
    WHERE s.channel='site' AND s.source_document_id=o.order_number
      AND s.source_line_id LIKE 'offer:'||NEW.id::TEXT||':%' AND finance_packaging_eligible(s)
    ON CONFLICT(sales_event_id) DO NOTHING;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS shipment_offers_finance_packaging_recognition ON shipment_offers;
CREATE TRIGGER shipment_offers_finance_packaging_recognition AFTER UPDATE OF status ON shipment_offers
FOR EACH ROW EXECUTE FUNCTION recognize_site_offer_packaging();

CREATE TABLE IF NOT EXISTS finance_marketplace_periods (
  id BIGSERIAL PRIMARY KEY,
  channel TEXT NOT NULL CHECK(channel IN ('ozon','wb','avito')),
  period_start DATE NOT NULL,
  period_end DATE NOT NULL CHECK(period_end>=period_start),
  source_document_id TEXT NOT NULL,
  source_hash TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'incomplete' CHECK(status IN ('incomplete','closed')),
  unmatched_rows INTEGER NOT NULL DEFAULT 0 CHECK(unmatched_rows>=0),
  confirmed_by BIGINT REFERENCES customers(id) ON DELETE SET NULL,
  confirmed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(channel,source_document_id)
);
CREATE TABLE IF NOT EXISTS finance_marketplace_report_rows (
  id BIGSERIAL PRIMARY KEY,
  period_id BIGINT NOT NULL REFERENCES finance_marketplace_periods(id) ON DELETE RESTRICT,
  source_line_id TEXT NOT NULL,
  sales_event_id BIGINT REFERENCES sales_events(id) ON DELETE SET NULL,
  row_kind TEXT NOT NULL DEFAULT 'primary' CHECK(row_kind IN ('primary','adjustment')),
  status TEXT NOT NULL DEFAULT 'matched' CHECK(status IN ('matched','unmatched')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(period_id,source_line_id)
);
ALTER TABLE finance_marketplace_adjustments
  ADD COLUMN IF NOT EXISTS report_row_id BIGINT REFERENCES finance_marketplace_report_rows(id) ON DELETE RESTRICT,
  ADD COLUMN IF NOT EXISTS linked_transaction_id BIGINT REFERENCES finance_transactions(id) ON DELETE SET NULL;

ALTER TABLE finance_imports
  ADD COLUMN IF NOT EXISTS opening_balance NUMERIC(14,2),
  ADD COLUMN IF NOT EXISTS incoming_total NUMERIC(14,2),
  ADD COLUMN IF NOT EXISTS outgoing_total NUMERIC(14,2),
  ADD COLUMN IF NOT EXISTS closing_balance NUMERIC(14,2),
  ADD COLUMN IF NOT EXISTS balance_valid BOOLEAN;

CREATE TABLE IF NOT EXISTS finance_classification_rules (
  id BIGSERIAL PRIMARY KEY,
  bank TEXT NOT NULL DEFAULT '',
  counterparty_pattern TEXT NOT NULL DEFAULT '',
  counterparty_account TEXT NOT NULL DEFAULT '',
  purpose_pattern TEXT NOT NULL DEFAULT '',
  classification TEXT NOT NULL,
  pnl_effect TEXT NOT NULL,
  created_from_transaction_id BIGINT REFERENCES finance_transactions(id) ON DELETE SET NULL,
  created_by BIGINT REFERENCES customers(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CHECK(bank<>'' OR counterparty_pattern<>'' OR counterparty_account<>'' OR purpose_pattern<>'')
);
