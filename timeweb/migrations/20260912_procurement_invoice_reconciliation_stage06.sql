-- Keep the buyer's plan immutable while invoice revisions are reconciled as
-- separate facts. Previous document revisions and excluded lines remain in
-- PostgreSQL for audit and rollback.
ALTER TABLE procurement_documents
  ADD COLUMN IF NOT EXISTS supersedes_document_id BIGINT REFERENCES procurement_documents(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS superseded_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS revision_no INTEGER NOT NULL DEFAULT 1 CHECK (revision_no > 0);

ALTER TABLE procurement_order_lines
  ADD COLUMN IF NOT EXISTS invoice_raw_name TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS invoice_supplier_article TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS reconciliation_status TEXT NOT NULL DEFAULT 'planned'
    CHECK (reconciliation_status IN ('planned','matched','changed','missing','added','excluded','superseded')),
  ADD COLUMN IF NOT EXISTS invoice_excluded BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS invoice_exclusion_reason TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS invoice_excluded_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS invoice_excluded_by BIGINT REFERENCES customers(id) ON DELETE SET NULL;

UPDATE procurement_order_lines SET
  invoice_raw_name=CASE WHEN procurement_document_id IS NOT NULL THEN raw_name ELSE '' END,
  invoice_supplier_article=CASE WHEN procurement_document_id IS NOT NULL THEN supplier_article ELSE '' END,
  reconciliation_status=CASE
    WHEN procurement_document_id IS NULL THEN 'planned'
    WHEN ordered_qty=0 THEN 'added'
    WHEN invoiced_qty IS DISTINCT FROM ordered_qty OR
      (expected_unit_price IS NOT NULL AND unit_price IS DISTINCT FROM expected_unit_price) THEN 'changed'
    ELSE 'matched' END
WHERE reconciliation_status='planned';

CREATE INDEX IF NOT EXISTS procurement_documents_active_order_idx
  ON procurement_documents(procurement_order_id, created_at DESC)
  WHERE superseded_at IS NULL;
CREATE INDEX IF NOT EXISTS procurement_lines_reconciliation_idx
  ON procurement_order_lines(procurement_order_id, reconciliation_status, id);

CREATE TABLE IF NOT EXISTS procurement_invoice_line_history (
  id BIGSERIAL PRIMARY KEY,
  procurement_order_line_id BIGINT NOT NULL REFERENCES procurement_order_lines(id) ON DELETE CASCADE,
  procurement_document_id BIGINT REFERENCES procurement_documents(id) ON DELETE SET NULL,
  invoice_raw_name TEXT NOT NULL DEFAULT '',
  invoice_supplier_article TEXT NOT NULL DEFAULT '',
  invoiced_qty INTEGER,
  unit_price NUMERIC(14,4),
  line_total NUMERIC(14,4),
  reconciliation_status TEXT NOT NULL,
  invoice_excluded BOOLEAN NOT NULL DEFAULT FALSE,
  invoice_exclusion_reason TEXT NOT NULL DEFAULT '',
  recorded_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS procurement_invoice_line_history_line_idx
  ON procurement_invoice_line_history(procurement_order_line_id, recorded_at DESC);
