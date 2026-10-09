-- Orders and shipment offers no longer reserve plants. Preserve the exact
-- local values before clearing them. Saby's available_qty is not changed.
-- This is a local accounting correction, not a stock movement to send to Saby.
-- The migration runner executes this file in one transaction.

CREATE TABLE IF NOT EXISTS reservation_reset_audit (
  entity_type TEXT NOT NULL,
  entity_key TEXT NOT NULL,
  quantity INTEGER NOT NULL,
  context JSONB NOT NULL DEFAULT '{}'::jsonb,
  reset_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (entity_type, entity_key)
);

-- Serialize against checkout, order edits and offer payment while taking the
-- snapshot, so no reservation can be lost between the audit and reset.
LOCK TABLE inventory, order_items, shipment_offer_items,
  shipment_offer_inventory_reservations IN ACCESS EXCLUSIVE MODE;

INSERT INTO reservation_reset_audit (entity_type, entity_key, quantity, context)
SELECT 'inventory', id::TEXT, reserved_qty,
  jsonb_build_object('warehouse_id', warehouse_id, 'variant_id', variant_id)
FROM inventory WHERE reserved_qty <> 0
ON CONFLICT (entity_type, entity_key) DO NOTHING;

INSERT INTO reservation_reset_audit (entity_type, entity_key, quantity, context)
SELECT 'order_item', id::TEXT, reserved_qty,
  jsonb_build_object('order_id', order_id, 'variant_id', variant_id)
FROM order_items WHERE reserved_qty <> 0
ON CONFLICT (entity_type, entity_key) DO NOTHING;

INSERT INTO reservation_reset_audit (entity_type, entity_key, quantity, context)
SELECT 'shipment_offer_item', id::TEXT, checkout_reserved_qty,
  jsonb_build_object('shipment_offer_id', shipment_offer_id, 'order_item_id', order_item_id)
FROM shipment_offer_items WHERE checkout_reserved_qty <> 0
ON CONFLICT (entity_type, entity_key) DO NOTHING;

INSERT INTO reservation_reset_audit (entity_type, entity_key, quantity, context)
SELECT 'shipment_offer_inventory',
  shipment_offer_item_id::TEXT || ':' || inventory_id::TEXT, quantity,
  jsonb_build_object('shipment_offer_item_id', shipment_offer_item_id, 'inventory_id', inventory_id)
FROM shipment_offer_inventory_reservations
ON CONFLICT (entity_type, entity_key) DO NOTHING;

-- Existing provider payment links are deliberately left intact: changing a
-- database status cannot cancel a live YooKassa payment. Capture old pending
-- amounts for release reconciliation without storing customer details here.
INSERT INTO reservation_reset_audit (entity_type, entity_key, quantity, context)
SELECT 'pending_payment', payment.id::TEXT, 0,
  jsonb_build_object('order_id', payment.order_id,
    'shipment_offer_id', payment.shipment_offer_id,
    'amount', payment.amount, 'plant_subtotal',
    COALESCE(offer.subtotal, purchase.subtotal),
    'has_provider_link', payment.confirmation_url <> '')
FROM payments payment
JOIN orders purchase ON purchase.id = payment.order_id
LEFT JOIN shipment_offers offer ON offer.id = payment.shipment_offer_id
WHERE payment.status = 'pending'
ON CONFLICT (entity_type, entity_key) DO NOTHING;

UPDATE inventory SET reserved_qty = 0 WHERE reserved_qty <> 0;
UPDATE order_items SET reserved_qty = 0 WHERE reserved_qty <> 0;
UPDATE shipment_offer_items SET checkout_reserved_qty = 0 WHERE checkout_reserved_qty <> 0;
DELETE FROM shipment_offer_inventory_reservations;
