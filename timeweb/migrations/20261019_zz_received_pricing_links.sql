-- Owner-confirmed catalog distribution for pricing an immutable receipt.
-- This table is never used by receiving, inventory, cost history or outbound jobs.
CREATE TABLE procurement_pricing_line_links (
 order_line_id BIGINT NOT NULL REFERENCES procurement_order_lines(id) ON DELETE CASCADE,
 saby_id TEXT NOT NULL REFERENCES saby_nomenclature(saby_id) ON DELETE RESTRICT,
 quantity INTEGER NOT NULL CHECK(quantity>0),
 requires_pim_package BOOLEAN NOT NULL DEFAULT FALSE,
 source TEXT NOT NULL CHECK(BTRIM(source)<>''),
 created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 PRIMARY KEY(order_line_id,saby_id)
);
COMMENT ON TABLE procurement_pricing_line_links IS 'Confirmed pricing-only distribution of historical received lines; not stock or receipt allocations';

-- 19 October: owner explicitly confirmed 8 calamondins, 6 limes, 6 lemons D9.
-- Select only the exact closed invoice and all three current, unique Saby codes.
WITH targets AS (
 SELECT seed.code,seed.quantity,MIN(n.saby_id) AS saby_id
 FROM (VALUES ('X615770892',8),('X525558640',6),('X740063870',6)) seed(code,quantity)
 JOIN saby_nomenclature n ON n.code=seed.code AND n.missing_since IS NULL
 GROUP BY seed.code,seed.quantity HAVING COUNT(*)=1
), invoice AS (
 SELECT MIN(l.id) AS id FROM procurement_order_lines l
 JOIN procurement_orders o ON o.id=l.procurement_order_id
 WHERE o.order_number='8208515' AND o.status='received'
 AND l.raw_name='Citr Mix' AND COALESCE(l.invoiced_qty,l.ordered_qty)=20
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
