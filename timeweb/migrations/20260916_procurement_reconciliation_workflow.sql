-- A row cannot be called matched when the buyer plan had no price to compare.
UPDATE procurement_order_lines
SET reconciliation_status='changed',comparison_accepted=FALSE,comparison_note='',updated_at=CURRENT_TIMESTAMP
WHERE ordered_qty>0 AND procurement_document_id IS NOT NULL
  AND reconciliation_status='matched' AND expected_unit_price IS NULL
  AND NOT invoice_excluded;
