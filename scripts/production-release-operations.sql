WITH
inventory_totals AS (
  SELECT variant_id, SUM(reserved_qty)::bigint AS quantity
  FROM inventory
  GROUP BY variant_id
),
order_totals AS (
  SELECT item.variant_id, SUM(item.reserved_qty)::bigint AS quantity
  FROM order_items item
  JOIN orders purchase ON purchase.id = item.order_id
  WHERE item.variant_id IS NOT NULL
    AND purchase.stock_released_at IS NULL
  GROUP BY item.variant_id
),
reservation_mismatches AS (
  SELECT COALESCE(inventory.variant_id, orders.variant_id) AS variant_id
  FROM inventory_totals inventory
  FULL OUTER JOIN order_totals orders USING (variant_id)
  WHERE COALESCE(inventory.quantity, 0) <> COALESCE(orders.quantity, 0)
)
SELECT code, severity, affected, oldest_seconds
FROM (
  SELECT 'reservation_ledger_mismatch'::text AS code, 'critical'::text AS severity,
         COUNT(*)::bigint AS affected, NULL::bigint AS oldest_seconds
  FROM reservation_mismatches
  UNION ALL
  SELECT 'negative_inventory_reservation', 'critical', COUNT(*)::bigint, NULL::bigint
  FROM inventory WHERE reserved_qty < 0
  UNION ALL
  SELECT 'duplicate_pending_payments', 'critical', COUNT(*)::bigint, NULL::bigint
  FROM (
    SELECT order_id FROM payments WHERE status = 'pending' AND provider_test IS DISTINCT FROM TRUE
    GROUP BY order_id HAVING COUNT(*) > 1
  ) duplicates
  UNION ALL
  SELECT 'provider_paid_order_unsettled', 'critical', COUNT(*)::bigint,
         EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - MIN(payment.updated_at)))::bigint
  FROM payments payment
  JOIN orders purchase ON purchase.id = payment.order_id
  WHERE payment.status = 'paid'
    AND payment.provider_test IS DISTINCT FROM TRUE
    AND purchase.payment_method = 'online'
    AND purchase.status <> 'cancelled'
    AND purchase.payment_status NOT IN ('paid', 'partially_paid', 'refunded')
  UNION ALL
  SELECT 'stock_reservation_journal_mismatch', 'warning', COUNT(*)::bigint, NULL::bigint
  FROM (
    SELECT item.order_id, item.variant_id
    FROM order_items item
    JOIN orders purchase ON purchase.id = item.order_id
    LEFT JOIN stock_movements movement
      ON movement.order_id = item.order_id
     AND movement.variant_id = item.variant_id
     AND movement.kind IN ('reserve', 'release')
    WHERE purchase.created_at >= CURRENT_TIMESTAMP - INTERVAL '30 days'
    GROUP BY item.order_id, item.variant_id
    HAVING MAX(CASE WHEN purchase.stock_released_at IS NULL THEN item.reserved_qty ELSE 0 END)
      <> COALESCE(SUM(CASE WHEN movement.kind = 'reserve' THEN movement.quantity ELSE -movement.quantity END), 0)
  ) journal_mismatches
  UNION ALL
  SELECT 'inventory_overreserved', 'warning', COUNT(*)::bigint, NULL::bigint
  FROM inventory WHERE reserved_qty > GREATEST(available_qty, 0)
  UNION ALL
  SELECT 'stale_pending_payment', 'warning', COUNT(*)::bigint,
         EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - MIN(payment.created_at)))::bigint
  FROM payments payment
  JOIN orders purchase ON purchase.id = payment.order_id
  WHERE payment.status = 'pending' AND purchase.status <> 'cancelled'
    AND payment.provider_test IS DISTINCT FROM TRUE
    AND payment.created_at < CURRENT_TIMESTAMP - INTERVAL '30 minutes'
  UNION ALL
  SELECT 'metrika_delivery_review', 'warning', COUNT(*)::bigint,
         EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - MIN(created_at)))::bigint
  FROM metrika_paid_outbox WHERE status IN ('failed','uncertain')
  UNION ALL
  SELECT 'metrika_delivery_overdue', 'warning', COUNT(*)::bigint,
         EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - MIN(created_at)))::bigint
  FROM metrika_paid_outbox WHERE status IN ('pending','sending') AND created_at<CURRENT_TIMESTAMP-INTERVAL '15 minutes'
  UNION ALL
  SELECT 'stale_outbox', 'warning', COUNT(*)::bigint,
         EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - MIN(created_at)))::bigint
  FROM outbox
  WHERE sent_at IS NULL AND cancelled_at IS NULL AND attempts < 5
    AND created_at < CURRENT_TIMESTAMP - INTERVAL '15 minutes'
  UNION ALL
  SELECT 'exhausted_outbox', 'warning', COUNT(*)::bigint,
         EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - MIN(created_at)))::bigint
  FROM outbox
  WHERE sent_at IS NULL AND cancelled_at IS NULL AND attempts >= 5
  UNION ALL
  SELECT 'cdek_manual_review', 'warning', COUNT(*)::bigint,
         EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - MIN(created_at)))::bigint
  FROM (
    SELECT created_at FROM orders
    WHERE cdek_create_state = 'manual_review' OR cdek_cancel_state = 'manual_review'
    UNION ALL
    SELECT created_at FROM shipment_offers WHERE cdek_create_state = 'manual_review'
  ) cdek_review
  UNION ALL
  SELECT 'cdek_retry_overdue', 'warning', COUNT(*)::bigint,
         EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - MIN(cdek_next_attempt_at)))::bigint
  FROM (
    SELECT cdek_next_attempt_at FROM orders
    WHERE cdek_create_state IN ('retry', 'unknown')
    UNION ALL
    SELECT cdek_next_attempt_at FROM shipment_offers
    WHERE cdek_create_state IN ('retry', 'unknown')
  ) cdek_retry
  WHERE cdek_next_attempt_at < CURRENT_TIMESTAMP - INTERVAL '5 minutes'
  UNION ALL
  SELECT CASE failed.channel
           WHEN 'wb' THEN 'failed_procurement_action_wb'
           WHEN 'ozon' THEN 'failed_procurement_action_ozon'
           WHEN 'saby_receipt' THEN 'failed_procurement_action_saby_receipt'
           WHEN 'saby_price' THEN 'failed_procurement_action_saby_price'
           ELSE 'failed_procurement_action_other'
         END,
         'warning', COUNT(*)::bigint,
         EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - MIN(failed.created_at)))::bigint
  FROM procurement_action_items failed
  JOIN procurement_action_batches failed_batch ON failed_batch.id = failed.batch_id
  WHERE failed.status = 'failed'
    AND failed_batch.status <> 'cancelled'
    AND NOT EXISTS (
      SELECT 1
      FROM procurement_action_items resolved
      WHERE resolved.procurement_order_line_id = failed.procurement_order_line_id
        AND resolved.channel = failed.channel
        AND resolved.id > failed.id
        AND resolved.status = 'completed'
    )
  GROUP BY 1
  UNION ALL
  SELECT 'payment_receipt_required', 'warning', COUNT(*)::bigint,
         EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - MIN(p.created_at)))::bigint
  FROM payments p
  WHERE p.status='paid' AND p.provider_payment_id<>''
    AND p.provider_test IS DISTINCT FROM TRUE
    AND p.created_at<CURRENT_TIMESTAMP-INTERVAL '15 minutes'
    AND (p.receipt_status IN ('canceled','unknown') OR (p.receipt_status IN ('','pending') AND p.created_at<CURRENT_TIMESTAMP-INTERVAL '3 days'))
  UNION ALL
  SELECT 'payment_settlement_receipt_required', 'warning', COUNT(*)::bigint,
         EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - MIN(p.created_at)))::bigint
  FROM payments p JOIN orders o ON o.id=p.order_id
  LEFT JOIN shipment_offers so ON so.id=p.shipment_offer_id
  LEFT JOIN payment_settlement_receipts sr ON sr.payment_id=p.id
  WHERE p.status='paid' AND p.provider_payment_id<>'' AND COALESCE(so.status,o.status)='completed'
    AND p.provider_test IS DISTINCT FROM TRUE
    AND payment_fiscal_mode(p.request_payload)<>'full_payment'
    AND ((sr.payment_id IS NULL AND (p.settlement_requested_at IS NULL OR p.settlement_requested_at<CURRENT_TIMESTAMP-INTERVAL '5 minutes'))
      OR sr.status IN ('failed','manual_review','canceled') OR (sr.status='pending' AND sr.created_at<CURRENT_TIMESTAMP-INTERVAL '3 days'))
    AND p.amount>COALESCE((SELECT SUM(r.amount) FROM payment_refunds r WHERE r.payment_id=p.id AND r.status='succeeded'),0)
  UNION ALL
  SELECT 'refund_manual_review', 'warning', COUNT(*)::bigint,
         EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - MIN(r.created_at)))::bigint
  FROM payment_refunds r JOIN payments p ON p.id=r.payment_id
  WHERE p.provider_test IS DISTINCT FROM TRUE AND (r.needs_review OR (r.status='pending' AND r.created_at<CURRENT_TIMESTAMP-INTERVAL '30 minutes'))
  UNION ALL
  SELECT 'refund_receipt_required', 'warning', COUNT(*)::bigint,
         EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - MIN(r.created_at)))::bigint
  FROM payment_refunds r JOIN payments p ON p.id=r.payment_id
  WHERE p.provider_test IS DISTINCT FROM TRUE AND r.status='succeeded' AND (r.receipt_status IN ('canceled','unknown','legacy_unverified') OR (r.receipt_status='pending' AND r.created_at<CURRENT_TIMESTAMP-INTERVAL '3 days'))
  UNION ALL
  SELECT 'expired_procurement_lock', 'warning', COUNT(*)::bigint,
         EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - MIN(locked_until)))::bigint
  FROM procurement_action_items
  WHERE status = 'processing' AND locked_until < CURRENT_TIMESTAMP
) checks
WHERE affected > 0
ORDER BY severity, code
