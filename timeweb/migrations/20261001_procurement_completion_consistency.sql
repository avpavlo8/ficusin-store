-- Bring already-posted procurements in line with the runtime rules.
-- A procurement title is independent from its workflow status: if a draft was
-- created before an invoice number was known, the invoice becomes its number.
UPDATE procurement_orders
SET order_number=document_number,updated_at=CURRENT_TIMESTAMP
WHERE BTRIM(order_number)='' AND BTRIM(document_number)<>'';

-- Existing deployments may already have a Saby receipt that was posted and
-- read back successfully while the procurement itself stayed ready_to_receive.
-- Fulfil allocated requests first, then close the procurement.
WITH completed_receipt_orders AS (
  SELECT DISTINCT batch.procurement_order_id
  FROM procurement_action_batches batch
  WHERE batch.kind='receipt' AND batch.procurement_order_id IS NOT NULL
    AND EXISTS (
      SELECT 1 FROM procurement_action_items item
      WHERE item.batch_id=batch.id AND item.channel='saby_receipt'
    )
    AND NOT EXISTS (
      SELECT 1 FROM procurement_action_items item
      WHERE item.batch_id=batch.id AND item.channel='saby_receipt'
        AND (item.status<>'completed' OR item.receipt_verified_at IS NULL)
    )
)
UPDATE procurement_request_allocations allocation
SET status='fulfilled',updated_at=CURRENT_TIMESTAMP
FROM procurement_order_lines line
JOIN procurement_orders orders ON orders.id=line.procurement_order_id
WHERE allocation.procurement_order_line_id=line.id
  AND allocation.status='active'
  AND orders.status='ready_to_receive'
  AND orders.id IN (SELECT procurement_order_id FROM completed_receipt_orders);

UPDATE procurement_requests request
SET status=CASE
    WHEN COALESCE(summary.fulfilled,0)>=request.quantity THEN 'fulfilled'
    WHEN COALESCE(summary.active,0)>0 THEN 'included'
    ELSE 'open'
  END,
  updated_at=CURRENT_TIMESTAMP
FROM (
  SELECT request_id,
    COALESCE(SUM(active_quantity) FILTER(WHERE status='active'),0) active,
    COALESCE(SUM(quantity) FILTER(WHERE status='fulfilled'),0) fulfilled
  FROM procurement_request_allocations
  GROUP BY request_id
) summary
WHERE request.id=summary.request_id
  AND request.status NOT IN('cancelled','fulfilled');

WITH completed_receipt_batches AS (
  SELECT batch.id,batch.procurement_order_id,MAX(item.receipt_verified_at) AS verified_at
  FROM procurement_action_batches batch
  JOIN procurement_action_items item ON item.batch_id=batch.id AND item.channel='saby_receipt'
  WHERE batch.kind='receipt' AND batch.procurement_order_id IS NOT NULL
  GROUP BY batch.id,batch.procurement_order_id
  HAVING COUNT(*) FILTER(WHERE item.status<>'completed' OR item.receipt_verified_at IS NULL)=0
), completed_receipt_orders AS (
  SELECT procurement_order_id,MAX(verified_at) AS verified_at
  FROM completed_receipt_batches
  GROUP BY procurement_order_id
)
UPDATE procurement_orders orders
SET status='received',
  received_at=COALESCE(orders.received_at,completed.verified_at,CURRENT_TIMESTAMP),
  updated_at=CURRENT_TIMESTAMP
FROM completed_receipt_orders completed
WHERE orders.id=completed.procurement_order_id
  AND orders.status='ready_to_receive';
