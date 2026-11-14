-- A zero-state cutover, not another reservation reset. Refuse deployment if
-- any old ownership remains; it must be reconciled explicitly beforehand.
LOCK TABLE public.orders,public.inventory,public.order_items,
 public.shipment_offer_items,public.shipment_offer_inventory_reservations
 IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM public.inventory WHERE reserved_qty<>0)
 OR EXISTS(SELECT 1 FROM public.order_items WHERE reserved_qty<>0)
 OR EXISTS(SELECT 1 FROM public.shipment_offer_items WHERE checkout_reserved_qty<>0)
 OR EXISTS(SELECT 1 FROM public.shipment_offer_inventory_reservations) THEN
  RAISE EXCEPTION 'Historical reservations require explicit reconciliation before cutover';
 END IF;
END $$;
CREATE SCHEMA IF NOT EXISTS legacy_archive;
CREATE TABLE legacy_archive.reservation_cutover_orders AS
 SELECT id AS order_id,stock_released_at,created_at AS original_created_at,
  CURRENT_TIMESTAMP AS archived_at FROM public.orders;
ALTER TABLE legacy_archive.reservation_cutover_orders ADD PRIMARY KEY(order_id);
CREATE TABLE legacy_archive.reservation_cutover_inventory AS
 SELECT id AS inventory_id,warehouse_id,variant_id,available_qty,reserved_qty,
  synced_at,stock_observed_at,CURRENT_TIMESTAMP AS archived_at FROM public.inventory;
ALTER TABLE legacy_archive.reservation_cutover_inventory ADD PRIMARY KEY(inventory_id);
CREATE TABLE legacy_archive.reservation_cutover_order_items AS
 SELECT id AS order_item_id,order_id,variant_id,quantity,reserved_qty,
  CURRENT_TIMESTAMP AS archived_at FROM public.order_items;
ALTER TABLE legacy_archive.reservation_cutover_order_items ADD PRIMARY KEY(order_item_id);
CREATE TABLE legacy_archive.reservation_cutover_offer_items AS
 SELECT id AS offer_item_id,shipment_offer_id,order_item_id,variant_id,
  checkout_reserved_qty,CURRENT_TIMESTAMP AS archived_at FROM public.shipment_offer_items;
ALTER TABLE legacy_archive.reservation_cutover_offer_items ADD PRIMARY KEY(offer_item_id);
CREATE TABLE legacy_archive.reservation_cutover_offer_allocations AS
 SELECT r.*,CURRENT_TIMESTAMP AS archived_at FROM public.shipment_offer_inventory_reservations r;
ALTER TABLE legacy_archive.reservation_cutover_offer_allocations
 ADD PRIMARY KEY(shipment_offer_item_id,inventory_id);

-- Retain compatible zero columns for current inventory readers. Cached old
-- producers cannot restore local reservations after the cutover check commits.
ALTER TABLE public.inventory ADD CONSTRAINT inventory_legacy_reservation_zero CHECK(reserved_qty=0);
ALTER TABLE public.order_items ADD CONSTRAINT order_items_legacy_reservation_zero CHECK(reserved_qty=0);
ALTER TABLE public.shipment_offer_items ADD CONSTRAINT shipment_offer_items_legacy_reservation_zero CHECK(checkout_reserved_qty=0);
ALTER TABLE public.shipment_offer_inventory_reservations ADD CONSTRAINT shipment_offer_inventory_reservations_retired CHECK(FALSE);
CREATE FUNCTION reject_retired_order_stock_release() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.stock_released_at IS DISTINCT FROM OLD.stock_released_at THEN
  RAISE EXCEPTION 'Local reservation release is retired; use the realization lifecycle';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER reject_retired_order_stock_release
 BEFORE UPDATE OF stock_released_at ON public.orders
 FOR EACH ROW EXECUTE FUNCTION reject_retired_order_stock_release();
-- No physical balance, payment, FIFO hold, realization proof or current order
-- status is modified. The detached archives survive active-row deletion.
