-- Repair the exact state produced when an invoice alias was unknown at import:
-- the buyer plan is "missing" and the invoice row is "added". Once both rows
-- resolve to one Saby item, collapse only unambiguous one-to-one pairs.
CREATE TEMP TABLE procurement_late_alias_pairs ON COMMIT DROP AS
WITH unique_added AS (
  SELECT line.procurement_order_id,line.saby_id,MIN(line.id) AS added_id
  FROM procurement_order_lines line
  JOIN procurement_supplier_aliases alias ON alias.id=line.supplier_alias_id
  JOIN procurement_orders orders ON orders.id=line.procurement_order_id
  WHERE line.saby_id IS NOT NULL
    AND alias.match_status='confirmed' AND alias.matched_saby_id=line.saby_id
    AND line.procurement_document_id IS NOT NULL AND line.source_line IS NOT NULL
    AND line.reconciliation_status='added' AND line.match_status='confirmed'
    AND NOT line.invoice_excluded AND orders.status NOT IN ('received','cancelled')
  GROUP BY line.procurement_order_id,line.saby_id
  HAVING COUNT(*)=1
), unique_missing AS (
  SELECT line.procurement_order_id,line.saby_id,MIN(line.id) AS planned_id
  FROM procurement_order_lines line
  JOIN procurement_orders orders ON orders.id=line.procurement_order_id
  WHERE line.saby_id IS NOT NULL AND line.procurement_document_id IS NULL
    AND line.ordered_qty>0 AND line.reconciliation_status='missing'
    AND orders.status NOT IN ('received','cancelled')
  GROUP BY line.procurement_order_id,line.saby_id
  HAVING COUNT(*)=1
)
SELECT added.procurement_order_id,added.added_id,missing.planned_id,
       invoice.procurement_document_id AS document_id,invoice.source_line
FROM unique_added added
JOIN unique_missing missing USING(procurement_order_id,saby_id)
JOIN procurement_order_lines invoice ON invoice.id=added.added_id;

-- The document/source-line pair is unique irrespective of reconciliation state,
-- so detach the superseded duplicate first and only then move provenance.
UPDATE procurement_order_lines stale SET
  procurement_document_id=NULL,reconciliation_status='superseded',updated_at=CURRENT_TIMESTAMP
FROM procurement_late_alias_pairs pair
WHERE stale.id=pair.added_id;

UPDATE procurement_order_lines planned SET
  procurement_document_id=pair.document_id,
  supplier_alias_id=invoice.supplier_alias_id,
  canonical_variant_id=COALESCE(invoice.canonical_variant_id,planned.canonical_variant_id),
  invoice_raw_name=invoice.invoice_raw_name,
  invoice_supplier_article=invoice.invoice_supplier_article,
  invoiced_qty=invoice.invoiced_qty,unit_price=invoice.unit_price,line_total=invoice.line_total,
  match_status=invoice.match_status,source_page=invoice.source_page,source_line=pair.source_line,
  comparison_accepted=FALSE,comparison_note='',
  reconciliation_status=CASE WHEN planned.ordered_qty IS DISTINCT FROM invoice.invoiced_qty
    OR (planned.expected_unit_price IS NOT NULL AND
      (invoice.unit_price IS NULL OR ABS(planned.expected_unit_price-invoice.unit_price)>.005))
    THEN 'changed' ELSE 'matched' END,
  updated_at=CURRENT_TIMESTAMP
FROM procurement_late_alias_pairs pair
JOIN procurement_order_lines invoice ON invoice.id=pair.added_id
WHERE planned.id=pair.planned_id;
