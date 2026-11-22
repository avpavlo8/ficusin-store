CREATE OR REPLACE VIEW procurement_catalog_arrivals AS
SELECT line.canonical_variant_id AS variant_id,
       MIN(purchase.expected_arrival_date) FILTER (
         WHERE purchase.expected_arrival_date >= (CURRENT_TIMESTAMP AT TIME ZONE 'Europe/Moscow')::DATE
       ) AS arrival_date
FROM procurement_order_lines line
JOIN procurement_orders purchase ON purchase.id=line.procurement_order_id
WHERE purchase.status IN ('ordered','invoice_received','review','ready_to_receive')
  AND line.canonical_variant_id IS NOT NULL AND line.match_status='confirmed'
  AND line.receiving_mode='catalog' AND NOT line.invoice_excluded
  AND line.reconciliation_status NOT IN ('missing','superseded')
  AND (line.reconciliation_status NOT IN ('added','changed') OR line.comparison_accepted)
  AND COALESCE(line.invoiced_qty,line.ordered_qty) > COALESCE((
    SELECT SUM(allocation.active_quantity)
    FROM procurement_request_allocations allocation
    WHERE allocation.procurement_order_line_id=line.id AND allocation.status='active'
  ),0)
GROUP BY line.canonical_variant_id;
