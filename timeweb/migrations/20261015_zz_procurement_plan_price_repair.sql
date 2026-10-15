-- Recover only an immutable, uninvoiced plan from its exact creation audit.
-- Never infer a supplier price from retail prices or change invoice/cost data.
WITH locked_orders AS MATERIALIZED (
  SELECT o.id, o.exchange_rate, o.currency
  FROM procurement_orders o
  WHERE o.status IN ('ordered','cancelled') AND o.calculated_at IS NOT NULL
    AND NOT EXISTS (SELECT 1 FROM procurement_documents d WHERE d.procurement_order_id=o.id)
  FOR UPDATE OF o
), source_audits AS (
  SELECT DISTINCT ON (o.id) o.id AS order_id, o.exchange_rate, o.currency,
    a.id AS audit_id, a.after_data->'items' AS items
  FROM locked_orders o
  JOIN admin_audit_log a ON a.entity_id=o.id::TEXT
    AND a.entity_type='procurement_order' AND a.action='procurement.plan.create'
  WHERE jsonb_typeof(a.after_data->'items')='array'
  ORDER BY o.id,a.id DESC
), planned_lines AS (
  SELECT l.*, ROW_NUMBER() OVER (PARTITION BY l.procurement_order_id ORDER BY l.id) AS ordinal,
    COUNT(*) OVER (PARTITION BY l.procurement_order_id) AS line_count
  FROM procurement_order_lines l JOIN locked_orders o ON o.id=l.procurement_order_id
), candidates AS (
  SELECT l.id, l.expected_unit_price AS old_price,
    (item.value->>'expectedUnitPrice')::NUMERIC AS price, a.audit_id
  FROM source_audits a
  CROSS JOIN LATERAL jsonb_array_elements(a.items) WITH ORDINALITY item(value,ordinal)
  JOIN planned_lines l ON l.procurement_order_id=a.order_id AND l.ordinal=item.ordinal
  WHERE l.line_count=jsonb_array_length(a.items)
    AND l.procurement_document_id IS NULL AND NOT l.invoice_excluded
    AND l.reconciliation_status='planned'
    AND COALESCE(l.saby_id,'')=COALESCE(item.value->>'sabyId','')
    AND l.raw_name=BTRIM(item.value->>'rawName')
    AND l.ordered_qty=(item.value->>'quantity')::INTEGER
    AND (item.value->>'expectedUnitPrice')::NUMERIC>0
    AND COALESCE(l.expected_unit_price,0)=TRUNC((item.value->>'expectedUnitPrice')::NUMERIC)
    AND l.expected_unit_price IS DISTINCT FROM (item.value->>'expectedUnitPrice')::NUMERIC
    AND l.purchase_unit_rub=ROUND((item.value->>'expectedUnitPrice')::NUMERIC
      * CASE WHEN a.currency='RUB' THEN 1 ELSE a.exchange_rate END,2)
), repaired AS (
  UPDATE procurement_order_lines l SET expected_unit_price=c.price,updated_at=CURRENT_TIMESTAMP
  FROM candidates c WHERE l.id=c.id
  RETURNING l.id,c.old_price,c.price,c.audit_id
)
INSERT INTO admin_audit_log(actor_customer_id,actor_role,action,entity_type,entity_id,before_data,after_data)
SELECT NULL,'system','procurement.plan.price.repair','procurement_order_line',id::TEXT,jsonb_build_object('expectedUnitPrice',old_price),
  jsonb_build_object('previousExpectedUnitPrice',old_price,'expectedUnitPrice',price,
    'sourceAuditId',audit_id,'reason','Fractional plan price recovered from exact creation audit')
FROM repaired;
