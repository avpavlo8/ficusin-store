-- Stage 05: auditable partial allocation of customer demand and manual
-- supplier availability follow-up. Existing rows remain valid.
CREATE TABLE IF NOT EXISTS procurement_request_allocations (
  id BIGSERIAL PRIMARY KEY,
  request_id BIGINT NOT NULL REFERENCES procurement_requests(id) ON DELETE CASCADE,
  procurement_order_line_id BIGINT NOT NULL REFERENCES procurement_order_lines(id) ON DELETE CASCADE,
  quantity INTEGER NOT NULL CHECK (quantity > 0),
  active_quantity INTEGER NOT NULL CHECK (active_quantity >= 0 AND active_quantity <= quantity),
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','released','fulfilled')),
  release_reason TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(request_id, procurement_order_line_id)
);
CREATE INDEX IF NOT EXISTS procurement_request_allocations_request_idx
  ON procurement_request_allocations(request_id, status);
CREATE INDEX IF NOT EXISTS procurement_request_allocations_line_idx
  ON procurement_request_allocations(procurement_order_line_id, status);

ALTER TABLE procurement_requests ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'manual';
ALTER TABLE procurement_supplier_products ADD COLUMN IF NOT EXISTS availability_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE procurement_supplier_products ADD COLUMN IF NOT EXISTS availability_comment TEXT NOT NULL DEFAULT '';
ALTER TABLE procurement_supplier_products ADD COLUMN IF NOT EXISTS availability_last_action TEXT NOT NULL DEFAULT '';
ALTER TABLE procurement_supplier_products ADD COLUMN IF NOT EXISTS availability_last_action_at TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS procurement_supplier_availability_events (
  id BIGSERIAL PRIMARY KEY,
  supplier_id BIGINT NOT NULL REFERENCES procurement_suppliers(id) ON DELETE CASCADE,
  saby_id TEXT NOT NULL REFERENCES saby_nomenclature(saby_id) ON DELETE CASCADE,
  status TEXT NOT NULL,
  reason TEXT NOT NULL DEFAULT '',
  comment TEXT NOT NULL DEFAULT '',
  check_after DATE,
  created_by BIGINT REFERENCES customers(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS procurement_supplier_availability_events_pair_idx
  ON procurement_supplier_availability_events(supplier_id, saby_id, created_at DESC);
