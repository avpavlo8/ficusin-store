-- One row is one physical plant. Marketplace money and the Saby receipt are
-- linked facts, never substitutes for the inspection history.
CREATE TABLE IF NOT EXISTS marketplace_returns (
  id BIGSERIAL PRIMARY KEY,
  channel TEXT NOT NULL CHECK(channel IN ('wb','ozon','avito','saby')),
  source_return_id TEXT NOT NULL DEFAULT '',
  source_shipment_id TEXT NOT NULL DEFAULT '',
  source_unit_index INTEGER NOT NULL DEFAULT 1 CHECK(source_unit_index > 0),
  sales_event_id BIGINT REFERENCES sales_events(id) ON DELETE SET NULL,
  canonical_variant_id BIGINT NOT NULL REFERENCES product_variants(id) ON DELETE RESTRICT,
  external_product_id TEXT NOT NULL DEFAULT '',
  returned_at DATE NOT NULL,
  condition TEXT NOT NULL DEFAULT 'inspection'
    CHECK(condition IN ('inspection','ready','restoring','dead')),
  comment TEXT NOT NULL DEFAULT '',
  unit_cost_rub_snapshot NUMERIC(14,2),
  cost_outcome TEXT NOT NULL DEFAULT 'unknown'
    CHECK(cost_outcome IN ('unknown','restored','lost')),
  financial_status TEXT NOT NULL DEFAULT 'incomplete'
    CHECK(financial_status IN ('linked','incomplete')),
  channel_withheld_rub NUMERIC(14,2),
  channel_compensation_rub NUMERIC(14,2),
  receipt_status TEXT NOT NULL DEFAULT 'none'
    CHECK(receipt_status IN ('none','queued','checking','draft_created','posted','failed','correction_required')),
  receipt_external_id TEXT NOT NULL DEFAULT '',
  receipt_external_url TEXT NOT NULL DEFAULT '',
  receipt_posted_at TIMESTAMPTZ,
  created_by BIGINT REFERENCES customers(id) ON DELETE SET NULL,
  updated_by BIGINT REFERENCES customers(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(channel,source_return_id,source_unit_index)
);
CREATE INDEX IF NOT EXISTS marketplace_returns_worklist_idx ON marketplace_returns(condition,returned_at DESC,id DESC);
CREATE INDEX IF NOT EXISTS marketplace_returns_sale_idx ON marketplace_returns(sales_event_id) WHERE sales_event_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS marketplace_return_history (
  id BIGSERIAL PRIMARY KEY,
  marketplace_return_id BIGINT NOT NULL REFERENCES marketplace_returns(id) ON DELETE CASCADE,
  from_condition TEXT NOT NULL DEFAULT '',
  to_condition TEXT NOT NULL,
  comment TEXT NOT NULL DEFAULT '',
  created_by BIGINT REFERENCES customers(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS marketplace_return_photos (
  id BIGSERIAL PRIMARY KEY,
  marketplace_return_id BIGINT NOT NULL REFERENCES marketplace_returns(id) ON DELETE CASCADE,
  content_type TEXT NOT NULL CHECK(content_type IN ('image/jpeg','image/png','image/webp')),
  image BYTEA NOT NULL,
  created_by BIGINT REFERENCES customers(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Return receipts reuse the coordinated procurement action worker and Saby
-- adapter. A return is not a procurement order, so both legacy parents become
-- optional and the dedicated return FK identifies the business operation.
ALTER TABLE procurement_action_items ALTER COLUMN batch_id DROP NOT NULL;
ALTER TABLE procurement_action_items ALTER COLUMN procurement_order_line_id DROP NOT NULL;
ALTER TABLE procurement_action_items ADD COLUMN IF NOT EXISTS marketplace_return_id BIGINT REFERENCES marketplace_returns(id) ON DELETE CASCADE;
CREATE UNIQUE INDEX IF NOT EXISTS marketplace_return_one_receipt_action_idx
  ON procurement_action_items(marketplace_return_id)
  WHERE marketplace_return_id IS NOT NULL AND channel='saby_receipt';
