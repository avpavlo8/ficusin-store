-- Исходный документ хранится в PostgreSQL: файловая система контейнера
-- эфемерна. Хэш делает повторную загрузку безопасной и идемпотентной.
CREATE TABLE IF NOT EXISTS procurement_documents (
  id BIGSERIAL PRIMARY KEY,
  supplier_id BIGINT NOT NULL REFERENCES procurement_suppliers(id),
  procurement_order_id BIGINT REFERENCES procurement_orders(id) ON DELETE SET NULL,
  file_name TEXT NOT NULL,
  content_type TEXT NOT NULL DEFAULT 'application/pdf',
  size_bytes INTEGER NOT NULL CHECK (size_bytes > 0),
  sha256 TEXT NOT NULL CHECK (LENGTH(sha256) = 64),
  content BYTEA NOT NULL,
  parser_kind TEXT NOT NULL DEFAULT 'unknown'
    CHECK (parser_kind IN ('unknown', 'holland_packing_list', 'domestic_payment_invoice')),
  parser_version INTEGER NOT NULL DEFAULT 1,
  parse_status TEXT NOT NULL DEFAULT 'uploaded'
    CHECK (parse_status IN ('uploaded', 'parsed', 'review', 'failed')),
  arithmetic_status TEXT NOT NULL DEFAULT 'unchecked'
    CHECK (arithmetic_status IN ('unchecked', 'ok', 'mismatch')),
  document_number TEXT NOT NULL DEFAULT '',
  document_date DATE,
  currency TEXT NOT NULL DEFAULT '' CHECK (currency IN ('', 'EUR', 'USD', 'RUB')),
  line_count INTEGER NOT NULL DEFAULT 0 CHECK (line_count >= 0),
  unit_count INTEGER NOT NULL DEFAULT 0 CHECK (unit_count >= 0),
  product_subtotal NUMERIC(14,4),
  package_total NUMERIC(14,4),
  document_total NUMERIC(14,4),
  calculated_total NUMERIC(14,4),
  extracted_text TEXT NOT NULL DEFAULT '',
  parse_error TEXT NOT NULL DEFAULT '',
  created_by BIGINT REFERENCES customers(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS procurement_documents_supplier_hash_unique
  ON procurement_documents (supplier_id, sha256);
CREATE INDEX IF NOT EXISTS procurement_documents_order_idx
  ON procurement_documents (procurement_order_id, created_at DESC);
CREATE INDEX IF NOT EXISTS procurement_documents_review_idx
  ON procurement_documents (parse_status, arithmetic_status, created_at DESC);

ALTER TABLE procurement_order_lines
  ADD COLUMN IF NOT EXISTS procurement_document_id BIGINT
    REFERENCES procurement_documents(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS source_page INTEGER CHECK (source_page > 0),
  ADD COLUMN IF NOT EXISTS source_line INTEGER CHECK (source_line > 0),
  ADD COLUMN IF NOT EXISTS line_total NUMERIC(14,4),
  ADD COLUMN IF NOT EXISTS pot_diameter_cm NUMERIC(8,2),
  ADD COLUMN IF NOT EXISTS height_cm NUMERIC(8,2);

CREATE UNIQUE INDEX IF NOT EXISTS procurement_order_lines_document_line_unique
  ON procurement_order_lines (procurement_document_id, source_line)
  WHERE procurement_document_id IS NOT NULL;
