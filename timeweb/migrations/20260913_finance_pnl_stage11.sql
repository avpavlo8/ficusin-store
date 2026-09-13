-- Stage 11: management P&L and the manual Dutch supplier account.
-- Bank balances remain cash facts and are deliberately not treated as a
-- balance sheet. Every rate and cost used by a historical row is snapshotted.

ALTER TABLE finance_transactions DROP CONSTRAINT IF EXISTS finance_transactions_classification_check;
ALTER TABLE finance_transactions ADD CONSTRAINT finance_transactions_classification_check
  CHECK(classification IN ('sale','marketplace_payout','acquiring','commission','supplier','tax','own_transfer','loan_principal','loan_interest','owner_contribution','owner_withdrawal','packaging_material','other','review'));

CREATE TABLE IF NOT EXISTS finance_packaging_snapshots (
  sales_event_id BIGINT PRIMARY KEY REFERENCES sales_events(id) ON DELETE CASCADE,
  units INTEGER NOT NULL CHECK(units >= 0),
  rate_rub NUMERIC(14,2) NOT NULL CHECK(rate_rub >= 0),
  amount_rub NUMERIC(14,2) NOT NULL CHECK(amount_rub >= 0),
  source TEXT NOT NULL DEFAULT 'normative_shipped_plant',
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE OR REPLACE FUNCTION snapshot_finance_packaging() RETURNS TRIGGER AS $$
BEGIN
  IF NEW.event_type='sale' AND NEW.event_status='confirmed'
     AND NEW.reconciliation_status='counted'
     AND NEW.channel IN ('site','ozon','wb') THEN
    INSERT INTO finance_packaging_snapshots(sales_event_id,units,rate_rub,amount_rub)
    VALUES(NEW.id,NEW.units,150,NEW.units*150)
    ON CONFLICT(sales_event_id) DO UPDATE SET
      units=EXCLUDED.units,
      amount_rub=EXCLUDED.units*finance_packaging_snapshots.rate_rub;
  ELSE
    DELETE FROM finance_packaging_snapshots WHERE sales_event_id=NEW.id;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS sales_events_finance_packaging ON sales_events;
CREATE TRIGGER sales_events_finance_packaging
AFTER INSERT OR UPDATE OF event_type,event_status,reconciliation_status,channel,units ON sales_events
FOR EACH ROW EXECUTE FUNCTION snapshot_finance_packaging();

INSERT INTO finance_packaging_snapshots(sales_event_id,units,rate_rub,amount_rub)
SELECT id,units,150,units*150 FROM sales_events
WHERE event_type='sale' AND event_status='confirmed'
  AND reconciliation_status='counted' AND channel IN ('site','ozon','wb')
ON CONFLICT(sales_event_id) DO NOTHING;

CREATE TABLE IF NOT EXISTS finance_marketplace_adjustments (
  id BIGSERIAL PRIMARY KEY,
  channel TEXT NOT NULL CHECK(channel IN ('ozon','wb','avito')),
  source_document_id TEXT NOT NULL,
  source_line_id TEXT NOT NULL DEFAULT '',
  sales_event_id BIGINT REFERENCES sales_events(id) ON DELETE SET NULL,
  operation_date DATE NOT NULL,
  kind TEXT NOT NULL CHECK(kind IN ('commission','logistics','withholding','compensation','acquiring','other')),
  amount_rub NUMERIC(14,2) NOT NULL CHECK(amount_rub >= 0),
  effect SMALLINT NOT NULL CHECK(effect IN (-1,1)),
  status TEXT NOT NULL DEFAULT 'review' CHECK(status IN ('review','confirmed')),
  source TEXT NOT NULL DEFAULT 'manual_report',
  created_by BIGINT REFERENCES customers(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(channel,source_document_id,source_line_id,kind)
);

CREATE TABLE IF NOT EXISTS finance_tax_periods (
  period_start DATE NOT NULL,
  period_end DATE NOT NULL,
  regime TEXT NOT NULL DEFAULT 'ausn_income_8',
  rate NUMERIC(7,6) NOT NULL DEFAULT 0.08,
  actual_tax_rub NUMERIC(14,2),
  source TEXT NOT NULL DEFAULT '',
  confirmed_by BIGINT REFERENCES customers(id) ON DELETE SET NULL,
  confirmed_at TIMESTAMPTZ,
  PRIMARY KEY(period_start,period_end),
  CHECK(period_end >= period_start),
  CHECK(actual_tax_rub IS NULL OR actual_tax_rub >= 0)
);

CREATE TABLE IF NOT EXISTS supplier_account_operations (
  id BIGSERIAL PRIMARY KEY,
  supplier TEXT NOT NULL DEFAULT 'flowersale_forever',
  operation_date DATE NOT NULL,
  kind TEXT NOT NULL CHECK(kind IN ('opening','topup','invoice','trolley_return','correction','other')),
  original_amount_eur NUMERIC(14,2) NOT NULL,
  normalized_amount_eur NUMERIC(14,2) NOT NULL,
  linked_topup_id BIGINT REFERENCES supplier_account_operations(id) ON DELETE RESTRICT,
  procurement_order_id BIGINT REFERENCES procurement_orders(id) ON DELETE RESTRICT,
  source_reference TEXT NOT NULL DEFAULT '',
  comment TEXT NOT NULL DEFAULT '',
  created_by BIGINT REFERENCES customers(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CHECK((kind='correction' AND linked_topup_id IS NOT NULL) OR (kind<>'correction' AND linked_topup_id IS NULL))
);
CREATE INDEX IF NOT EXISTS supplier_account_operations_date_idx
  ON supplier_account_operations(operation_date DESC,id DESC);
CREATE UNIQUE INDEX IF NOT EXISTS supplier_account_operations_document_idx
  ON supplier_account_operations(supplier,kind,source_reference)
  WHERE source_reference<>'' AND kind IN ('invoice','trolley_return');

CREATE TABLE IF NOT EXISTS supplier_account_reconciliations (
  id BIGSERIAL PRIMARY KEY,
  supplier TEXT NOT NULL DEFAULT 'flowersale_forever',
  reconciliation_date DATE NOT NULL,
  original_balance_eur NUMERIC(14,2) NOT NULL,
  normalized_balance_eur NUMERIC(14,2) NOT NULL,
  reserved_eur NUMERIC(14,2),
  source_reference TEXT NOT NULL DEFAULT '',
  comment TEXT NOT NULL DEFAULT '',
  created_by BIGINT REFERENCES customers(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
