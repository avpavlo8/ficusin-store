-- The supplier plan name and the invoice name are separate fields. Match the
-- invoice label shown to the operator, retaining all receipt/cost records.
WITH targets AS (
 SELECT seed.code,seed.quantity,MIN(n.saby_id) AS saby_id
 FROM (VALUES ('X615770892',8),('X525558640',6),('X740063870',6)) seed(code,quantity)
 JOIN saby_nomenclature n ON n.code=seed.code AND n.missing_since IS NULL
 GROUP BY seed.code,seed.quantity HAVING COUNT(*)=1
), invoice AS (
 SELECT MIN(l.id) AS id FROM procurement_order_lines l
 JOIN procurement_orders o ON o.id=l.procurement_order_id
 WHERE o.order_number='8208515' AND o.status='received'
 AND l.invoice_raw_name='Citr Mix' AND COALESCE(l.invoiced_qty,l.ordered_qty)=20
 AND l.receiving_parent_line_id IS NULL AND l.canonical_variant_id IS NULL
 AND l.receiving_mode='catalog' AND l.match_status='confirmed'
 AND NOT l.invoice_excluded AND l.reconciliation_status<>'superseded'
 HAVING COUNT(*)=1
)
INSERT INTO procurement_pricing_line_links(order_line_id,saby_id,quantity,requires_pim_package,source)
-- Ozon currently says 150x150x50 mm for a 20-30 cm plant. Require a
-- manager-confirmed PIM box instead of accepting that impossible MP input.
SELECT invoice.id,t.saby_id,t.quantity,TRUE,'Состав 8/6/6 подтверждён владельцем 19.10.2026; только расчёт цен'
FROM invoice CROSS JOIN targets t
WHERE invoice.id IS NOT NULL AND (SELECT COUNT(*) FROM targets)=3
ON CONFLICT DO NOTHING;

ALTER TABLE procurement_action_batches ADD COLUMN unified_price_snapshot JSONB;
COMMENT ON COLUMN procurement_action_batches.unified_price_snapshot IS 'Current unified MP quote and mapping snapshot; approval and worker both revalidate it';

-- One historical mix row can now produce independent price actions per SKU.
-- Keep the legacy uniqueness rule for all previous receipt and price actions.
ALTER TABLE procurement_action_items ADD COLUMN pricing_variant_id BIGINT REFERENCES product_variants(id) ON DELETE RESTRICT;
ALTER TABLE procurement_action_items ADD CONSTRAINT procurement_pricing_variant_channel CHECK(pricing_variant_id IS NULL OR channel IN ('wb','ozon'));
ALTER TABLE procurement_action_items DROP CONSTRAINT procurement_action_items_batch_id_procurement_order_line_id_key;
CREATE UNIQUE INDEX procurement_action_items_legacy_unique ON procurement_action_items(batch_id,procurement_order_line_id,channel) WHERE pricing_variant_id IS NULL;
CREATE UNIQUE INDEX procurement_action_items_unified_unique ON procurement_action_items(batch_id,pricing_variant_id,channel) WHERE pricing_variant_id IS NOT NULL;
