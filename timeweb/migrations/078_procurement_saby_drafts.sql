-- A Saby draft is one external operation containing every line of a
-- procurement document. Keep the immutable request snapshot in the durable
-- action queue so a restart or retry cannot create a different receipt.
ALTER TABLE procurement_action_items
  ADD COLUMN IF NOT EXISTS payload JSONB NOT NULL DEFAULT '{}'::JSONB,
  ADD COLUMN IF NOT EXISTS external_url TEXT NOT NULL DEFAULT '';

ALTER TABLE procurement_suppliers
  ADD COLUMN IF NOT EXISTS tax_id TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS procurement_suppliers_tax_id_unique
  ON procurement_suppliers (tax_id) WHERE tax_id <> '';

UPDATE procurement_suppliers
SET tax_id = '7627031650', updated_at = CURRENT_TIMESTAMP
WHERE tax_id = '' AND LOWER(name) IN (
  LOWER('ТК Ярославский, ООО'),
  LOWER('ООО «ТК Ярославский»'),
  LOWER('ООО "ТК Ярославский"')
);
