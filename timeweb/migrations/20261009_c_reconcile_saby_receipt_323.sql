-- П3-12963: the attempted receipt linked to deleted document 351 failed.
-- The owner identified Saby receipt 323 (28.08.2026); its posted screen shows
-- the same supplier, all five products and all 65 units. Cancel only the
-- obsolete action batch. Keep the failed item and its original external ID
-- for diagnosis; do not call Saby, change stock, or record actual unit costs.
DO $$
DECLARE
  matched_count INTEGER;
  target RECORD;
  actual_lines TEXT[];
BEGIN
  SELECT COUNT(*) INTO matched_count
  FROM procurement_action_items item
  JOIN procurement_action_batches batch ON batch.id=item.batch_id
  JOIN procurement_orders orders ON orders.id=batch.procurement_order_id
  WHERE orders.order_number='П3-12963' AND item.channel='saby_receipt';

  IF matched_count=0 THEN
    -- Fresh databases do not contain this historical procurement.
    RETURN;
  END IF;
  IF matched_count<>1 THEN
    RAISE EXCEPTION 'П3-12963: expected one historical Saby receipt action, got %', matched_count;
  END IF;

  SELECT item.id AS item_id, item.batch_id, item.status AS item_status,
         item.quantity, item.payload, batch.status AS batch_status,
         orders.status AS order_status, orders.id AS order_id
  INTO target
  FROM procurement_action_items item
  JOIN procurement_action_batches batch ON batch.id=item.batch_id
  JOIN procurement_orders orders ON orders.id=batch.procurement_order_id
  WHERE orders.order_number='П3-12963' AND item.channel='saby_receipt'
  FOR UPDATE OF item, batch;

  IF target.batch_status='cancelled' THEN
    RETURN;
  END IF;
  SELECT ARRAY(
    SELECT (line->>'sabyId') || ':' || (line->>'quantity')
    FROM jsonb_array_elements(COALESCE(target.payload->'lines', '[]'::jsonb)) line
    ORDER BY line->>'sabyId'
  ) INTO actual_lines;
  IF target.item_status<>'failed' OR target.quantity<>65 OR target.order_status<>'received'
      OR actual_lines<>ARRAY['2542:20','2971:20','3604:3','3605:2','631:20']::TEXT[] THEN
    RAISE EXCEPTION 'П3-12963: historical receipt differs from verified Saby document 323';
  END IF;

  INSERT INTO admin_audit_log(actor_customer_id,actor_role,action,entity_type,entity_id,before_data,after_data)
  VALUES(NULL,'system','procurement.receipt.reconciled_external','procurement_action_batch',target.batch_id::TEXT,
    jsonb_build_object('batchStatus',target.batch_status,'itemStatus',target.item_status,
      'itemId',target.item_id,'orderId',target.order_id,'lines',actual_lines),
    jsonb_build_object('batchStatus','cancelled','reason','receipt_already_posted_in_saby',
      'externalDocumentNumber','323','externalDocumentDate','2026-08-28',
      'supplier','ТК Ярославский, ООО','postedUnits',65,'lines',actual_lines,
      'sabyUnitCosts','zero_on_screen','siteStockChanged',false,'siteCostsChanged',false));

  UPDATE procurement_action_batches
  SET status='cancelled', updated_at=CURRENT_TIMESTAMP
  WHERE id=target.batch_id;
END $$;
