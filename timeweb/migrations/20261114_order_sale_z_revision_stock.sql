-- A document can be unposted/reposted many times. Attribute a cached stock
-- balance to the last verified effect before collection START, not to the
-- publication's first posting timestamp. Keep added units' own FIFO priority.
CREATE INDEX order_sale_revision_effects_frontier_idx ON order_sale_revision_effects(intent_id,verified_at DESC,id DESC);
CREATE INDEX order_sale_hold_layers_stock_idx ON order_sale_hold_layers(variant_id,intent_id,admitted_at,id);
CREATE INDEX order_sale_hold_layers_owner_idx ON order_sale_hold_layers(intent_id,variant_id,admitted_at,id);
CREATE VIEW order_sale_stock_hold_layers AS
WITH layers AS (
 SELECT l.intent_id,i.order_id,i.warehouse_id,l.variant_id,l.id AS layer_id,
  l.admitted_at,GREATEST(0,l.quantity-COALESCE(released.quantity,0))::BIGINT AS quantity
 FROM order_sale_hold_layers l JOIN order_sale_intents i ON i.id=l.intent_id
 LEFT JOIN LATERAL(SELECT SUM(r.quantity) AS quantity FROM order_sale_hold_releases r WHERE r.layer_id=l.id) released ON TRUE
 WHERE i.event_trigger='created'
  AND NOT(i.cancellation_verified AND NOT EXISTS(SELECT 1 FROM order_sale_publications p WHERE p.intent_id=i.id))
  AND NOT EXISTS(SELECT 1 FROM order_sale_publications p WHERE p.intent_id=i.id AND p.cancellation_verified
   AND (p.cancellation_proof->>'kind'='no_remote' OR NOT p.post_attempted))
 UNION ALL
 -- Old cohorts remain protected before the first edit seeds the ledger.
 SELECT i.id,i.order_id,i.warehouse_id,l.variant_id,-l.order_item_id,
  i.created_at,l.quantity::BIGINT
 FROM order_sale_intents i JOIN order_sale_intent_lines l ON l.intent_id=i.id
 WHERE i.event_trigger='created' AND i.base_revision_id IS NULL
  AND NOT i.cancellation_verified
  AND NOT EXISTS(SELECT 1 FROM order_sale_publications p WHERE p.intent_id=i.id AND p.cancellation_verified)
), totals AS (
 SELECT l.*,p.execution_mode,stock.stock_observed_at,
  CASE WHEN latest.id IS NOT NULL THEN CASE WHEN latest.posted THEN COALESCE(current_line.quantity,0) ELSE 0 END
   WHEN COALESCE(p.stock_verified,FALSE) AND NOT COALESCE(p.cancellation_verified,FALSE) THEN COALESCE(original.quantity,0) ELSE 0 END::BIGINT AS native_posted,
  CASE WHEN p.execution_mode='live' THEN
   CASE WHEN reflected.id IS NOT NULL THEN CASE WHEN reflected.posted THEN COALESCE(reflected_line.quantity,0) ELSE 0 END
    WHEN NOT EXISTS(SELECT 1 FROM order_sale_revision_effects any_effect WHERE any_effect.intent_id=l.intent_id)
      AND p.stock_verified_at<stock.stock_observed_at AND COALESCE(p.stock_verified,FALSE)
      AND NOT (p.cancellation_verified AND p.cancellation_verified_at<stock.stock_observed_at)
     THEN COALESCE(original.quantity,0) ELSE 0 END
   ELSE 0 END::BIGINT AS native_reflected,
  CASE WHEN latest.operation='revision_unpost' AND
    (stock.stock_observed_at IS NULL OR stock.stock_observed_at<=latest.verified_at)
   THEN COALESCE(current_line.quantity,0) ELSE 0 END::BIGINT AS unpost_credit
 FROM layers l
 LEFT JOIN order_sale_publications p ON p.intent_id=l.intent_id
 LEFT JOIN inventory stock ON stock.warehouse_id=l.warehouse_id AND stock.variant_id=l.variant_id
 LEFT JOIN LATERAL(SELECT SUM(x.quantity)::BIGINT AS quantity FROM order_sale_intent_lines x WHERE x.intent_id=l.intent_id AND x.variant_id=l.variant_id) original ON TRUE
 LEFT JOIN LATERAL(SELECT e.* FROM order_sale_revision_effects e WHERE e.intent_id=l.intent_id ORDER BY e.verified_at DESC,e.id DESC LIMIT 1) latest ON TRUE
 LEFT JOIN order_sale_revision_effect_lines current_line ON current_line.effect_id=latest.id AND current_line.variant_id=l.variant_id
 LEFT JOIN LATERAL(SELECT e.* FROM order_sale_revision_effects e WHERE e.intent_id=l.intent_id AND e.verified_at<stock.stock_observed_at ORDER BY e.verified_at DESC,e.id DESC LIMIT 1) reflected ON TRUE
 LEFT JOIN order_sale_revision_effect_lines reflected_line ON reflected_line.effect_id=reflected.id AND reflected_line.variant_id=l.variant_id
), prefixes AS (
 SELECT t.*,COALESCE(SUM(quantity) OVER(PARTITION BY intent_id,variant_id ORDER BY admitted_at,layer_id ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING),0) AS earlier_owned
 FROM totals t WHERE quantity>0
)
SELECT intent_id,order_id,warehouse_id,variant_id,layer_id,admitted_at,quantity,
 GREATEST(0,LEAST(quantity,native_reflected-earlier_owned))::BIGINT AS reflected_quantity,
 (quantity-GREATEST(0,LEAST(quantity,native_reflected-earlier_owned)))::BIGINT AS pending_quantity,
 native_posted AS posted_quantity,unpost_credit
FROM prefixes;

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

-- Release newest admitted units first. No future-dated release is recorded:
-- a changed target can still retain already held units before the safe boundary.
-- Importers hold inventory rows, so SKIP LOCKED must avoid waiting on an edit
-- which holds the intent and may itself need the inventory row.
CREATE FUNCTION order_sale_refresh_hold_releases(intent_id_arg BIGINT DEFAULT NULL)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE owner RECORD; stock RECORD; effect RECORD; layer RECORD;
 desired_qty BIGINT; active_qty BIGINT; posted_qty BIGINT; remaining BIGINT;
 excess BIGINT; release_qty BIGINT; inserted INTEGER;
BEGIN
 FOR owner IN SELECT i.* FROM order_sale_intents i WHERE i.event_trigger='created'
  AND i.base_revision_id IS NOT NULL AND (intent_id_arg IS NULL OR i.id=intent_id_arg)
  ORDER BY i.id FOR UPDATE SKIP LOCKED
 LOOP
  SELECT e.*,p.execution_mode INTO effect FROM order_sale_revision_effects e
   JOIN order_sale_publications p ON p.intent_id=e.intent_id
   WHERE e.intent_id=owner.id ORDER BY e.verified_at DESC,e.id DESC LIMIT 1;
  IF NOT FOUND THEN CONTINUE; END IF;
  FOR stock IN SELECT inv.* FROM inventory inv WHERE inv.warehouse_id=owner.warehouse_id
   AND EXISTS(SELECT 1 FROM order_sale_hold_layers l WHERE l.intent_id=owner.id AND l.variant_id=inv.variant_id)
   ORDER BY inv.variant_id
  LOOP
   IF effect.execution_mode<>'mock' AND (stock.stock_observed_at IS NULL OR stock.stock_observed_at<=effect.verified_at) THEN CONTINUE; END IF;
   desired_qty:=0; active_qty:=0; posted_qty:=0;
   IF NOT owner.cancellation_verified THEN
    SELECT COALESCE(SUM(quantity),0) INTO desired_qty FROM order_sale_revision_lines WHERE revision_id=owner.desired_revision_id AND variant_id=stock.variant_id;
    SELECT COALESCE(SUM(quantity),0) INTO active_qty FROM order_sale_revision_lines WHERE revision_id=owner.active_revision_id AND variant_id=stock.variant_id;
    IF effect.posted THEN SELECT COALESCE(SUM(quantity),0) INTO posted_qty FROM order_sale_revision_effect_lines WHERE effect_id=effect.id AND variant_id=stock.variant_id; END IF;
   END IF;
   SELECT COALESCE(SUM(l.quantity-COALESCE((SELECT SUM(r.quantity) FROM order_sale_hold_releases r WHERE r.layer_id=l.id),0)),0)
    INTO remaining FROM order_sale_hold_layers l WHERE l.intent_id=owner.id AND l.variant_id=stock.variant_id;
   excess:=GREATEST(0,remaining-GREATEST(desired_qty,active_qty,posted_qty));
   FOR layer IN SELECT l.*,GREATEST(0,l.quantity-COALESCE((SELECT SUM(r.quantity) FROM order_sale_hold_releases r WHERE r.layer_id=l.id),0)) AS remaining
    FROM order_sale_hold_layers l WHERE l.intent_id=owner.id AND l.variant_id=stock.variant_id ORDER BY l.admitted_at DESC,l.id DESC
   LOOP
    EXIT WHEN excess<=0;
    release_qty:=LEAST(excess,layer.remaining);
    IF release_qty<=0 THEN CONTINUE; END IF;
    INSERT INTO order_sale_hold_releases(layer_id,effect_id,quantity) VALUES(layer.id,effect.id,release_qty) ON CONFLICT(layer_id,effect_id) DO NOTHING;
    GET DIAGNOSTICS inserted=ROW_COUNT;
    IF inserted=1 THEN excess:=excess-release_qty; END IF;
   END LOOP;
  END LOOP;
 END LOOP;
END $$;

CREATE FUNCTION order_sale_inventory_revision_watermark() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE intent BIGINT;
BEGIN
 IF TG_OP='UPDATE' AND NEW.stock_observed_at IS NOT DISTINCT FROM OLD.stock_observed_at THEN RETURN NEW; END IF;
 FOR intent IN SELECT DISTINCT l.intent_id FROM order_sale_hold_layers l JOIN order_sale_intents i ON i.id=l.intent_id
  WHERE l.variant_id=NEW.variant_id AND i.warehouse_id=NEW.warehouse_id ORDER BY l.intent_id
 LOOP PERFORM order_sale_refresh_hold_releases(intent); END LOOP;
 RETURN NEW;
END $$;
CREATE TRIGGER order_sale_inventory_revision_watermark AFTER INSERT OR UPDATE OF stock_observed_at ON inventory
 FOR EACH ROW EXECUTE FUNCTION order_sale_inventory_revision_watermark();
