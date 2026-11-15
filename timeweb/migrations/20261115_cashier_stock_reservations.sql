-- Cash-at-counter orders reserve availability but never create a site sale.
-- Historical remote outcomes must be reconciled before enrolling that cohort.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM order_sale_publications p JOIN order_sale_intents i ON i.id=p.intent_id
   JOIN orders o ON o.id=i.order_id WHERE o.payment_method='on_delivery' AND p.execution_mode='live'
   AND (p.create_attempted OR p.post_attempted OR p.document_id<>'' OR p.document_uuid<>'' OR p.stock_verified)) THEN
  RAISE EXCEPTION 'Cashier cutover requires reconciliation of existing live site realizations';
 END IF;
END $$;

-- Protect rolling deployments and older clients too: an obsolete publisher
-- cannot create a second sale for an order whose write-off belongs to the till.
CREATE FUNCTION reject_cashier_site_realization() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM orders WHERE id=NEW.order_id AND payment_method='on_delivery') THEN
  RAISE EXCEPTION 'Cashier owns this sale; a site realization would risk a second write-off';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER reject_cashier_site_realization BEFORE INSERT ON order_sale_intents
 FOR EACH ROW EXECUTE FUNCTION reject_cashier_site_realization();

CREATE TABLE cashier_stock_reservations (
 id BIGSERIAL PRIMARY KEY,
 order_id BIGINT NOT NULL UNIQUE REFERENCES orders(id) ON DELETE RESTRICT,
 warehouse_id BIGINT NOT NULL REFERENCES warehouses(id) ON DELETE RESTRICT,
 admitted_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 completed_at TIMESTAMPTZ,
 cancelled_at TIMESTAMPTZ,
 CHECK(completed_at IS NULL OR cancelled_at IS NULL)
);
CREATE TABLE cashier_stock_layers (
 id BIGSERIAL PRIMARY KEY,
 reservation_id BIGINT NOT NULL REFERENCES cashier_stock_reservations(id) ON DELETE RESTRICT,
 variant_id BIGINT NOT NULL REFERENCES product_variants(id) ON DELETE RESTRICT,
 quantity INTEGER NOT NULL CHECK(quantity>0),
 released_quantity INTEGER NOT NULL DEFAULT 0 CHECK(released_quantity>=0 AND released_quantity<=quantity),
 admitted_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX cashier_stock_layers_owner ON cashier_stock_layers(reservation_id,variant_id,id);

-- Counts are reconciled from live order rows after each edit. Added units get a
-- new FIFO layer; a reduction releases the newest units without ageing them.
CREATE FUNCTION cashier_stock_sync(order_id_arg BIGINT) RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE owner RECORD; reservation RECORD; item RECORD; layer RECORD;
 site_warehouse BIGINT; warehouse_count INTEGER; current_qty BIGINT; excess BIGINT; amount INTEGER;
BEGIN
 SELECT * INTO owner FROM orders WHERE id=order_id_arg FOR UPDATE;
 IF NOT FOUND OR owner.payment_method IS DISTINCT FROM 'on_delivery' THEN RETURN; END IF;
 SELECT * INTO reservation FROM cashier_stock_reservations WHERE order_id=order_id_arg FOR UPDATE;
 IF NOT FOUND THEN
  IF owner.status IN ('completed','cancelled') THEN RETURN; END IF;
  IF EXISTS(SELECT 1 FROM order_sale_intents WHERE order_id=order_id_arg) THEN
   RAISE EXCEPTION 'Cashier reservation requires reconciliation of the existing realization intent';
  END IF;
  SELECT MIN(id),COUNT(*) INTO site_warehouse,warehouse_count FROM warehouses
   WHERE saby_id='saby-ryazan-main' AND is_active<>0;
  IF warehouse_count<>1 THEN RAISE EXCEPTION 'Cashier reservations require one confirmed site warehouse'; END IF;
  INSERT INTO cashier_stock_reservations(order_id,warehouse_id)
   VALUES(order_id_arg,site_warehouse) RETURNING * INTO reservation;
 END IF;
 IF owner.status='cancelled' THEN
  IF reservation.completed_at IS NOT NULL THEN RAISE EXCEPTION 'Issued cashier order requires a physical return'; END IF;
  UPDATE cashier_stock_reservations SET cancelled_at=COALESCE(cancelled_at,clock_timestamp()) WHERE id=reservation.id;
  RETURN;
 END IF;
 IF reservation.cancelled_at IS NOT NULL THEN RAISE EXCEPTION 'Cancelled cashier reservation cannot be reopened'; END IF;
 IF owner.status='completed' THEN
  IF owner.payment_status<>'paid' THEN RAISE EXCEPTION 'Cashier handover requires confirmed counter payment'; END IF;
  UPDATE cashier_stock_reservations SET completed_at=COALESCE(completed_at,clock_timestamp()) WHERE id=reservation.id;
  RETURN;
 END IF;
 IF reservation.completed_at IS NOT NULL THEN RAISE EXCEPTION 'Issued cashier order cannot be reopened'; END IF;
 FOR item IN SELECT variant_id,COALESCE(SUM(quantity),0)::BIGINT AS desired FROM order_items
   WHERE order_id=order_id_arg AND variant_id IS NOT NULL GROUP BY variant_id
  UNION ALL SELECT l.variant_id,0::BIGINT FROM cashier_stock_layers l WHERE l.reservation_id=reservation.id
   AND NOT EXISTS(SELECT 1 FROM order_items oi WHERE oi.order_id=order_id_arg AND oi.variant_id=l.variant_id)
   GROUP BY l.variant_id ORDER BY variant_id
 LOOP
  PERFORM pg_advisory_xact_lock(hashtextextended('order-stock:'||item.variant_id::BIGINT::TEXT,0));
  SELECT COALESCE(SUM(quantity-released_quantity),0) INTO current_qty FROM cashier_stock_layers
   WHERE reservation_id=reservation.id AND variant_id=item.variant_id;
  IF item.desired>current_qty THEN
   INSERT INTO cashier_stock_layers(reservation_id,variant_id,quantity) VALUES(reservation.id,item.variant_id,item.desired-current_qty);
  ELSIF item.desired<current_qty THEN
   excess:=current_qty-item.desired;
   FOR layer IN SELECT * FROM cashier_stock_layers WHERE reservation_id=reservation.id
    AND variant_id=item.variant_id AND released_quantity<quantity ORDER BY id DESC FOR UPDATE
   LOOP
    EXIT WHEN excess=0;
    amount:=LEAST(excess,layer.quantity-layer.released_quantity)::INTEGER;
    UPDATE cashier_stock_layers SET released_quantity=released_quantity+amount WHERE id=layer.id;
    excess:=excess-amount;
   END LOOP;
  END IF;
 END LOOP;
END $$;

CREATE FUNCTION cashier_stock_items_changed() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM cashier_stock_reservations r
  WHERE r.order_id=CASE WHEN TG_OP='DELETE' THEN OLD.order_id ELSE NEW.order_id END
   AND r.completed_at IS NOT NULL) THEN
  RAISE EXCEPTION 'Issued cashier composition requires a physical return';
 END IF;
 IF TG_OP='DELETE' THEN PERFORM cashier_stock_sync(OLD.order_id);
 ELSE
  IF TG_OP='UPDATE' AND NEW.order_id IS DISTINCT FROM OLD.order_id THEN
   RAISE EXCEPTION 'Cannot move cashier order lines between orders';
  END IF;
  PERFORM cashier_stock_sync(NEW.order_id);
 END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER cashier_stock_items_changed AFTER INSERT OR UPDATE OR DELETE ON order_items
 FOR EACH ROW EXECUTE FUNCTION cashier_stock_items_changed();
CREATE FUNCTION cashier_stock_order_changed() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.payment_method='on_delivery' AND NEW.payment_method IS DISTINCT FROM OLD.payment_method THEN
  RAISE EXCEPTION 'Cashier payment-method change requires explicit stock reconciliation';
 END IF;
 PERFORM cashier_stock_sync(NEW.id);
 RETURN NULL;
END $$;
CREATE TRIGGER cashier_stock_order_changed AFTER UPDATE OF status,payment_method ON orders
 FOR EACH ROW EXECUTE FUNCTION cashier_stock_order_changed();

-- Enrol only open cash orders without a previous site intent. Never manufacture
-- a receipt, reopen an old order, unpost a document, or alter physical inventory.
DO $$ DECLARE item RECORD; BEGIN
 FOR item IN SELECT id FROM orders WHERE payment_method='on_delivery' AND status NOT IN ('cancelled','completed') ORDER BY id
 LOOP PERFORM cashier_stock_sync(item.id); END LOOP;
END $$;

ALTER VIEW order_sale_stock_hold_layers RENAME TO order_sale_realization_hold_layers;
CREATE VIEW order_sale_stock_hold_layers AS
 SELECT * FROM order_sale_realization_hold_layers
 UNION ALL
 SELECT -r.id AS intent_id,r.order_id,r.warehouse_id,l.variant_id,-l.id AS layer_id,l.admitted_at,
  (l.quantity-l.released_quantity)::BIGINT AS quantity,
  CASE WHEN r.completed_at IS NOT NULL AND stock.stock_observed_at>r.completed_at
   THEN (l.quantity-l.released_quantity)::BIGINT ELSE 0::BIGINT END AS reflected_quantity,
  CASE WHEN r.completed_at IS NOT NULL AND stock.stock_observed_at>r.completed_at
   THEN 0::BIGINT ELSE (l.quantity-l.released_quantity)::BIGINT END AS pending_quantity,
  0::BIGINT AS posted_quantity,0::BIGINT AS unpost_credit
 FROM cashier_stock_reservations r JOIN cashier_stock_layers l ON l.reservation_id=r.id
 LEFT JOIN inventory stock ON stock.warehouse_id=r.warehouse_id AND stock.variant_id=l.variant_id
 WHERE r.cancelled_at IS NULL AND l.quantity>l.released_quantity;

-- Keep the old projection available for diagnostics. A partly reflected layer
-- is split instead of claiming that the whole layer was reflected.
CREATE OR REPLACE VIEW order_sale_stock_holds AS
 SELECT h.intent_id,h.order_id,h.warehouse_id,h.variant_id,
  LEAST(2147483647,part.quantity)::INTEGER AS quantity,
  LEAST(2147483647,CASE WHEN part.reflected THEN part.quantity ELSE 0 END)::INTEGER AS posted_quantity,
  part.reflected
 FROM order_sale_stock_hold_layers h
 CROSS JOIN LATERAL(VALUES(h.reflected_quantity,TRUE),(h.pending_quantity,FALSE)) part(quantity,reflected)
 WHERE part.quantity>0;

CREATE OR REPLACE VIEW order_sale_inventory AS
 SELECT stock.id,stock.warehouse_id,stock.variant_id,
  GREATEST(-2147483648,LEAST(2147483647,stock.available_qty::BIGINT-COALESCE(held.quantity,0)))::INTEGER AS available_qty,
  stock.reserved_qty,stock.synced_at,stock.stock_observed_at
 FROM inventory stock LEFT JOIN LATERAL(
  SELECT SUM(h.pending_quantity) AS quantity FROM order_sale_stock_hold_layers h
  WHERE h.variant_id=stock.variant_id AND h.warehouse_id=stock.warehouse_id
 ) held ON TRUE;

CREATE OR REPLACE FUNCTION order_sale_owned_quantity(order_id_arg BIGINT,warehouse_id_arg BIGINT,variant_id_arg BIGINT)
RETURNS INTEGER LANGUAGE SQL STABLE AS $$
 WITH ordered AS (
  SELECT h.*,COALESCE(SUM(pending_quantity) OVER(ORDER BY admitted_at,intent_id,layer_id ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING),0) AS earlier
  FROM order_sale_stock_hold_layers h WHERE warehouse_id=warehouse_id_arg AND variant_id=variant_id_arg
 ), totals AS (
  SELECT COALESCE(SUM(pending_quantity),0) AS pending,
   COALESCE(SUM(GREATEST(0,LEAST(pending_quantity,stock.available_qty::BIGINT-stock.reserved_qty-earlier))+reflected_quantity) FILTER(WHERE order_id=order_id_arg),0) AS allocated,
   COALESCE(MAX(posted_quantity) FILTER(WHERE order_id=order_id_arg),0) AS posted,
   COALESCE(MAX(unpost_credit) FILTER(WHERE order_id=order_id_arg),0) AS unposted,
   COUNT(*) FILTER(WHERE order_id=order_id_arg) AS owned
  FROM inventory stock LEFT JOIN ordered ON TRUE
  WHERE stock.warehouse_id=warehouse_id_arg AND stock.variant_id=variant_id_arg
  GROUP BY stock.available_qty,stock.reserved_qty
 ) SELECT CASE WHEN totals.owned=0 THEN 0 ELSE
  LEAST(2147483647,GREATEST(0,GREATEST(totals.allocated,totals.posted,totals.unposted)
   -LEAST(0,GREATEST(-2147483648,LEAST(2147483647,stock.available_qty::BIGINT-totals.pending))-stock.reserved_qty)))::INTEGER END
 FROM inventory stock CROSS JOIN totals
 WHERE stock.warehouse_id=warehouse_id_arg AND stock.variant_id=variant_id_arg
$$;

