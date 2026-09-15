-- Stage 07: current cost is a versioned fact. The initial Saby retail/2 value
-- is an explicit estimate from this date forward; it is never backdated.
ALTER TABLE product_variants
  ADD COLUMN IF NOT EXISTS current_unit_cost_rub NUMERIC(18,6),
  ADD COLUMN IF NOT EXISTS current_unit_cost_kind TEXT
    CHECK (current_unit_cost_kind IN ('estimated','actual')),
  ADD COLUMN IF NOT EXISTS current_unit_cost_effective_at TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS procurement_cost_history (
  id BIGSERIAL PRIMARY KEY,
  canonical_variant_id BIGINT NOT NULL REFERENCES product_variants(id) ON DELETE RESTRICT,
  saby_id TEXT NOT NULL REFERENCES saby_nomenclature(saby_id) ON DELETE RESTRICT,
  procurement_order_id BIGINT REFERENCES procurement_orders(id) ON DELETE RESTRICT,
  procurement_order_line_id BIGINT REFERENCES procurement_order_lines(id) ON DELETE RESTRICT,
  unit_cost_rub NUMERIC(18,6) NOT NULL CHECK (unit_cost_rub >= 0),
  cost_kind TEXT NOT NULL CHECK (cost_kind IN ('estimated','actual')),
  source TEXT NOT NULL,
  effective_at TIMESTAMPTZ NOT NULL,
  recorded_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS procurement_cost_history_initial_estimate_unique
  ON procurement_cost_history(canonical_variant_id, cost_kind)
  WHERE cost_kind='estimated';
CREATE UNIQUE INDEX IF NOT EXISTS procurement_cost_history_receipt_unique
  ON procurement_cost_history(procurement_order_id, canonical_variant_id)
  WHERE cost_kind='actual';
CREATE INDEX IF NOT EXISTS procurement_cost_history_effective_idx
  ON procurement_cost_history(canonical_variant_id, effective_at DESC, id DESC);

INSERT INTO procurement_cost_history(
  canonical_variant_id,saby_id,unit_cost_rub,cost_kind,source,effective_at
)
SELECT variant.id,variant.saby_id,(n.price_minor::NUMERIC/200),'estimated',
  'saby_retail_half_initial',CURRENT_TIMESTAMP
FROM product_variants variant
JOIN saby_nomenclature n ON n.saby_id=variant.saby_id
WHERE variant.saby_id IS NOT NULL AND n.price_minor>0
ON CONFLICT DO NOTHING;

UPDATE product_variants variant SET
  current_unit_cost_rub=history.unit_cost_rub,
  current_unit_cost_kind=history.cost_kind,
  current_unit_cost_effective_at=history.effective_at
FROM procurement_cost_history history
WHERE history.canonical_variant_id=variant.id
  AND history.cost_kind='estimated' AND variant.current_unit_cost_rub IS NULL;

ALTER TABLE sales_events
  ADD COLUMN IF NOT EXISTS unit_cost_rub_snapshot NUMERIC(18,6),
  ADD COLUMN IF NOT EXISTS cost_quality TEXT
    CHECK (cost_quality IN ('estimated','actual')),
  ADD COLUMN IF NOT EXISTS cost_history_id BIGINT REFERENCES procurement_cost_history(id) ON DELETE RESTRICT;

ALTER TABLE procurement_action_items
  ADD COLUMN IF NOT EXISTS receipt_verified_at TIMESTAMPTZ;
