-- Russian legal entities are addressed in Saby by INN + KPP.
ALTER TABLE procurement_suppliers
  ADD COLUMN IF NOT EXISTS kpp TEXT NOT NULL DEFAULT '';

UPDATE procurement_suppliers
SET tax_id = '7627031650',
    kpp = '762701001',
    updated_at = CURRENT_TIMESTAMP
WHERE tax_id = '7627031650'
   OR LOWER(name) LIKE '%ярославск%';

-- Repair snapshots prepared before KPP was stored. A manual retry reuses the
-- same complete document and does not create stock movements on the site.
UPDATE procurement_action_items
SET payload = jsonb_set(
      jsonb_set(payload, '{supplier,taxId}', to_jsonb('7627031650'::TEXT), TRUE),
      '{supplier,kpp}', to_jsonb('762701001'::TEXT), TRUE
    ),
    updated_at = CURRENT_TIMESTAMP
WHERE channel = 'saby_receipt'
  AND (
    payload #>> '{supplier,taxId}' = '7627031650'
    OR LOWER(COALESCE(payload #>> '{supplier,name}', '')) LIKE '%ярославск%'
  )
  AND COALESCE(payload #>> '{supplier,kpp}', '') = '';
