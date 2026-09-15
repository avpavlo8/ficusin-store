-- Stage 10: an auditable business ledger. Imported source rows are immutable;
-- corrections are append-only classification versions.
CREATE TABLE IF NOT EXISTS finance_imports (
  id BIGSERIAL PRIMARY KEY,
  bank TEXT NOT NULL CHECK(bank IN ('tochka','sber','ozon')),
  account_number TEXT NOT NULL DEFAULT '',
  file_name TEXT NOT NULL,
  file_sha256 TEXT NOT NULL,
  source_file BYTEA NOT NULL,
  source_format TEXT NOT NULL CHECK(source_format IN ('pdf','xlsx')),
  status TEXT NOT NULL DEFAULT 'preview' CHECK(status IN ('preview','confirmed','failed')),
  rows_total INTEGER NOT NULL DEFAULT 0,
  rows_new INTEGER NOT NULL DEFAULT 0,
  rows_duplicate INTEGER NOT NULL DEFAULT 0,
  rows_review INTEGER NOT NULL DEFAULT 0,
  created_by BIGINT REFERENCES customers(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  confirmed_at TIMESTAMPTZ,
  UNIQUE(bank,account_number,file_sha256)
);

CREATE TABLE IF NOT EXISTS finance_transactions (
  id BIGSERIAL PRIMARY KEY,
  import_id BIGINT NOT NULL REFERENCES finance_imports(id) ON DELETE RESTRICT,
  bank TEXT NOT NULL,
  account_number TEXT NOT NULL DEFAULT '',
  source_row INTEGER NOT NULL,
  operation_date DATE NOT NULL,
  document_number TEXT NOT NULL DEFAULT '',
  counterparty TEXT NOT NULL DEFAULT '',
  counterparty_account TEXT NOT NULL DEFAULT '',
  purpose TEXT NOT NULL DEFAULT '',
  debit_rub NUMERIC(14,2) NOT NULL DEFAULT 0 CHECK(debit_rub >= 0),
  credit_rub NUMERIC(14,2) NOT NULL DEFAULT 0 CHECK(credit_rub >= 0),
  currency TEXT NOT NULL DEFAULT 'RUB',
  dedupe_key TEXT NOT NULL,
  classification TEXT NOT NULL DEFAULT 'review' CHECK(classification IN ('sale','marketplace_payout','acquiring','commission','supplier','tax','own_transfer','loan_principal','loan_interest','owner_contribution','owner_withdrawal','other','review')),
  pnl_effect TEXT NOT NULL DEFAULT 'review' CHECK(pnl_effect IN ('income','expense','none','review')),
  review_reason TEXT NOT NULL DEFAULT '',
  confirmed BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(bank,account_number,dedupe_key)
);
CREATE INDEX IF NOT EXISTS finance_transactions_period_idx ON finance_transactions(operation_date DESC,id DESC);
CREATE INDEX IF NOT EXISTS finance_transactions_review_idx ON finance_transactions(classification) WHERE classification='review';

CREATE TABLE IF NOT EXISTS finance_classification_versions (
  id BIGSERIAL PRIMARY KEY,
  transaction_id BIGINT NOT NULL REFERENCES finance_transactions(id) ON DELETE CASCADE,
  version INTEGER NOT NULL,
  classification TEXT NOT NULL,
  pnl_effect TEXT NOT NULL,
  reason TEXT NOT NULL DEFAULT '',
  changed_by BIGINT REFERENCES customers(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(transaction_id,version)
);

CREATE TABLE IF NOT EXISTS finance_cash_entries (
  id BIGSERIAL PRIMARY KEY,
  operation_date DATE NOT NULL,
  kind TEXT NOT NULL CHECK(kind IN ('opening','saby_sale','expense','bank_transfer','adjustment')),
  direction TEXT NOT NULL CHECK(direction IN ('in','out')),
  amount_rub NUMERIC(14,2) NOT NULL CHECK(amount_rub > 0),
  purpose TEXT NOT NULL DEFAULT '',
  linked_transaction_id BIGINT REFERENCES finance_transactions(id) ON DELETE SET NULL,
  created_by BIGINT REFERENCES customers(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS finance_cash_entries_linked_transaction_idx
  ON finance_cash_entries(linked_transaction_id) WHERE linked_transaction_id IS NOT NULL;
CREATE TABLE IF NOT EXISTS finance_cash_reconciliations (
  id BIGSERIAL PRIMARY KEY,
  reconciliation_date DATE NOT NULL,
  expected_rub NUMERIC(14,2) NOT NULL,
  actual_rub NUMERIC(14,2) NOT NULL,
  comment TEXT NOT NULL DEFAULT '',
  created_by BIGINT REFERENCES customers(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
