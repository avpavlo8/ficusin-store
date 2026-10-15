-- Keep every original invoice row when several rows fulfil one planned item.
ALTER TABLE procurement_order_lines ADD COLUMN invoice_components JSONB NOT NULL DEFAULT '[]'::JSONB;
ALTER TABLE procurement_invoice_line_history ADD COLUMN invoice_components JSONB NOT NULL DEFAULT '[]'::JSONB;
ALTER TABLE procurement_order_lines ALTER COLUMN unit_price TYPE NUMERIC(20,10);

-- Recover exact plan aliases left unresolved by the old manual invoice picker.
-- Require an unambiguous confirmed card and the full supplier/article/size key.
WITH evidence AS (
 SELECT a.id,MIN(l.saby_id) AS saby_id,MIN(l.canonical_variant_id) AS variant_id
 FROM procurement_supplier_aliases a
 JOIN procurement_orders o ON o.supplier_id=a.supplier_id AND o.status<>'cancelled'
 JOIN procurement_order_lines l ON l.procurement_order_id=o.id
   AND LOWER(l.raw_name)=LOWER(a.raw_name)
   AND COALESCE(l.supplier_article,'')=COALESCE(a.supplier_article,'')
   AND l.pot_diameter_cm IS NOT DISTINCT FROM a.pot_diameter_cm
   AND l.height_cm IS NOT DISTINCT FROM a.height_cm
 WHERE a.match_status IN ('unmatched','suggested','new_product')
   AND l.ordered_qty>0 AND l.match_status='confirmed' AND l.saby_id IS NOT NULL
   AND l.procurement_document_id IS NOT NULL AND l.reconciliation_status IN ('matched','changed')
 GROUP BY a.id HAVING COUNT(DISTINCT l.saby_id)=1 AND COUNT(DISTINCT l.canonical_variant_id)<=1
)
UPDATE procurement_supplier_aliases a SET matched_saby_id=e.saby_id,canonical_variant_id=e.variant_id,
 match_status='confirmed',confidence=1,updated_at=CURRENT_TIMESTAMP
FROM evidence e WHERE a.id=e.id;
