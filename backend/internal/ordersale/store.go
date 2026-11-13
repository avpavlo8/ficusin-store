package ordersale

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"sort"
)

type queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func readEvent(ctx context.Context, q queryer, orderID, offerID int64) (Event, error) {
	e := Event{Key: eventKey(orderID, offerID), OrderID: orderID, ShipmentOfferID: offerID, Lines: []Line{}, State: "not_prepared"}
	var stored []byte
	err := q.QueryRow(ctx, `SELECT i.id,i.snapshot,i.state,i.source,COALESCE(i.warehouse_id,0),i.prepared_at,i.created_at,i.event_trigger,(i.cancellation_requested_at IS NOT NULL),i.cancellation_blocked FROM order_sale_intents i WHERE event_key=$1 AND order_id=$2 AND COALESCE(shipment_offer_id,0)=$3`, e.Key, orderID, offerID).Scan(&e.IntentID, &stored, &e.State, &e.Source, &e.WarehouseID, &e.PreparedAt, &e.CreatedAt, &e.Trigger, &e.CancellationRequested, &e.CancellationBlocked)
	if err == nil {
		var original Event
		if err = json.Unmarshal(stored, &original); err != nil {
			return e, err
		}
		if e.Trigger == "handover" {
			e.Trigger = ""
		}
		e.OrderNumber = original.OrderNumber
		e.DeliveryMethod = original.DeliveryMethod
		e.Status = original.Status
		e.CDEKStatus = original.CDEKStatus
		e.Lines = original.Lines
		e.QuantityConflict = original.QuantityConflict
		// Frozen quantities/prices belong to the actual handover. Until preparation,
		// a missing catalogue link may be resolved without changing those quantities.
		for n := range e.Lines {
			var current string
			if err = q.QueryRow(ctx, `SELECT COALESCE((SELECT CASE WHEN COUNT(DISTINCT external_id)=1 THEN MIN(external_id) ELSE '' END FROM product_external_ids WHERE variant_id=$1 AND provider='saby' AND id_type='id' AND status='active' HAVING COUNT(*)>0),(SELECT saby_id FROM product_variants WHERE id=$1),'')`, e.Lines[n].VariantID).Scan(&current); err != nil {
				return e, err
			}
			if e.Lines[n].SabyID != "" && current != e.Lines[n].SabyID {
				e.MappingConflict = true
			}
			if e.State == "queued" && e.Lines[n].SabyID == "" {
				e.Lines[n].SabyID = current
			}
		}

		// Handover eligibility is frozen with the intent, not inferred from a later status toggle.
		finish(&e)
		e.Publication, err = loadPublication(ctx, q, e.IntentID)
		return e, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return e, err
	}
	if offerID > 0 {
		err = q.QueryRow(ctx, `SELECT o.order_number,so.delivery_method,so.status,so.cdek_status,so.created_at FROM shipment_offers so JOIN orders o ON o.id=so.order_id WHERE so.id=$1 AND o.id=$2`, offerID, orderID).Scan(&e.OrderNumber, &e.DeliveryMethod, &e.Status, &e.CDEKStatus, &e.CreatedAt)
	} else {
		err = q.QueryRow(ctx, `SELECT order_number,delivery_method,status,COALESCE(cdek_status,''),created_at FROM orders WHERE id=$1 AND NOT EXISTS(SELECT 1 FROM shipment_offers so WHERE so.order_id=$1 AND so.status NOT IN ('cancelled','expired','stale'))`, orderID).Scan(&e.OrderNumber, &e.DeliveryMethod, &e.Status, &e.CDEKStatus, &e.CreatedAt)
	}
	if err != nil {
		return e, err
	}
	rows, err := q.Query(ctx, `WITH source AS (
 SELECT oi.id AS item_id,COALESCE(oi.variant_id,0) AS variant_id,oi.product_name,COALESCE(oi.sku,'') AS sku,oi.quantity,oi.unit_price::TEXT AS price FROM order_items oi WHERE oi.order_id=$1 AND $2::BIGINT=0
 UNION ALL
 SELECT soi.order_item_id,COALESCE(soi.variant_id,0),soi.product_name,soi.sku,soi.quantity,soi.unit_price::TEXT FROM shipment_offer_items soi JOIN shipment_offers so ON so.id=soi.shipment_offer_id WHERE so.order_id=$1 AND soi.shipment_offer_id=$2
 ) SELECT s.item_id,s.variant_id,s.product_name,s.sku,COALESCE((SELECT CASE WHEN COUNT(DISTINCT external_id)=1 THEN MIN(external_id) ELSE '' END FROM product_external_ids WHERE variant_id=s.variant_id AND provider='saby' AND id_type='id' AND status='active' HAVING COUNT(*)>0),(SELECT saby_id FROM product_variants WHERE id=s.variant_id),''),
 LEAST(s.quantity,GREATEST(0,oi.quantity-COALESCE((SELECT SUM(l.quantity) FROM order_sale_intent_lines l WHERE l.order_item_id=s.item_id),0)))::INTEGER,s.price,s.quantity
 FROM source s JOIN order_items oi ON oi.id=s.item_id ORDER BY s.item_id`, orderID, offerID)
	if err != nil {
		return e, err
	}
	defer rows.Close()
	for rows.Next() {
		var l Line
		var actual int
		if err = rows.Scan(&l.OrderItemID, &l.VariantID, &l.Name, &l.SKU, &l.SabyID, &l.Quantity, &l.UnitPrice, &actual); err != nil {
			return e, err
		}
		if offerID > 0 && actual > l.Quantity {
			e.QuantityConflict = true
			l.Quantity = actual
		}
		if l.Quantity > 0 {
			e.Lines = append(e.Lines, l)
		}
	}
	if err = rows.Err(); err != nil {
		return e, err
	}
	finish(&e)
	return e, nil
}

// Enqueue must share the transaction that records the verified milestone. It
// only records an intent; missing Saby/warehouse metadata never rejects handover.
func Enqueue(ctx context.Context, tx pgx.Tx, orderID, offerID int64, source string) (Event, error) {
	if source != "automatic" && source != "manual" {
		return Event{}, fmt.Errorf("invalid realization source")
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('order-sale:'||$1::BIGINT::TEXT,0))`, orderID); err != nil {
		return Event{}, err
	}
	// The whole new order already owns its quantities. Later physical milestones
	// must never create a second realization for a partial shipment.
	var createdID int64
	err := tx.QueryRow(ctx, `SELECT id FROM order_sale_intents WHERE order_id=$1 AND event_trigger='created'`, orderID).Scan(&createdID)
	if err == nil {
		return readEvent(ctx, tx, orderID, 0)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Event{}, err
	}
	e, err := readEvent(ctx, tx, orderID, offerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Event{}, nil
	}
	if err != nil {
		return e, err
	}
	if e.IntentID > 0 {
		return e, nil
	}
	if !HandedOver(e.DeliveryMethod, e.Status, e.CDEKStatus) || (source == "automatic" && e.DeliveryMethod == "cdek" && e.Status != "shipped") {
		return e, nil
	}
	// Persist even a blocked/missing-link event: it must remain visible to the operator.
	e.Source = source
	e.State = "queued"
	raw, err := json.Marshal(e)
	if err != nil {
		return e, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO order_sale_intents(event_key,order_id,shipment_offer_id,source,snapshot) VALUES($1,$2,NULLIF($3,0),$4,$5) RETURNING id`, e.Key, orderID, offerID, source, raw).Scan(&e.IntentID)
	if err != nil {
		return e, err
	}
	for _, l := range e.Lines {
		if e.QuantityConflict {
			break
		}
		if _, err = tx.Exec(ctx, `INSERT INTO order_sale_intent_lines(intent_id,order_item_id,quantity) VALUES($1,$2,$3)`, e.IntentID, l.OrderItemID, l.Quantity); err != nil {
			return e, err
		}
	}
	finish(&e)
	if source == "automatic" {
		if err = ensurePublication(ctx, tx, &e, 0, false); err != nil {
			return e, err
		}
	}
	return e, nil
}

func List(ctx context.Context, pool *pgxpool.Pool, search string, offset int) (View, error) {
	view := View{Events: []Event{}, Warehouses: []Warehouse{}}
	control, err := readDispatchControl(ctx, pool, false)
	if err != nil {
		return view, err
	}
	view.ExecutionMode = control.Mode
	view.DispatchEnabled = control.Enabled
	view.DispatchMode = control.Mode
	view.AutoDispatchEnabled = control.Enabled && control.SiteWarehouseID > 0
	view.ExternalWritesEnabled = control.Enabled && control.Mode == "live"
	rows, err := pool.Query(ctx, `SELECT id,name FROM warehouses WHERE is_active<>0 ORDER BY name,id`)
	if err != nil {
		return view, err
	}
	for rows.Next() {
		var w Warehouse
		if err = rows.Scan(&w.ID, &w.Name); err != nil {
			rows.Close()
			return view, err
		}
		w.NativeMappingConfirmed = control.Enabled && control.Mode == "live" && control.NativeWarehouseID > 0 && w.ID == control.SiteWarehouseID
		view.Warehouses = append(view.Warehouses, w)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return view, err
	}
	rows, err = pool.Query(ctx, `WITH events AS (
 SELECT o.id AS order_id,0::BIGINT AS offer_id,o.order_number,o.created_at FROM orders o WHERE o.status IN ('shipped','completed') AND NOT EXISTS(SELECT 1 FROM shipment_offers so WHERE so.order_id=o.id AND so.status NOT IN ('cancelled','expired','stale'))
 UNION SELECT so.order_id,so.id,o.order_number,so.created_at FROM shipment_offers so JOIN orders o ON o.id=so.order_id WHERE so.status IN ('shipped','completed') AND NOT EXISTS(SELECT 1 FROM order_sale_intents ci WHERE ci.order_id=o.id AND ci.event_trigger='created')
 UNION SELECT i.order_id,COALESCE(i.shipment_offer_id,0),o.order_number,i.created_at FROM order_sale_intents i JOIN orders o ON o.id=i.order_id
 ) SELECT order_id,offer_id FROM events WHERE ($1='' OR order_number ILIKE '%'||$1||'%') GROUP BY order_id,offer_id ORDER BY MAX(created_at) DESC,order_id DESC,offer_id DESC LIMIT 51 OFFSET $2`, search, offset)
	if err != nil {
		return view, err
	}
	type ids struct{ o, s int64 }
	keys := []ids{}
	for rows.Next() {
		var id ids
		if err = rows.Scan(&id.o, &id.s); err != nil {
			rows.Close()
			return view, err
		}
		keys = append(keys, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return view, err
	}
	view.HasMore = len(keys) > 50
	if view.HasMore {
		keys = keys[:50]
	}
	for _, id := range keys {
		event, e := readEvent(ctx, pool, id.o, id.s)
		if errors.Is(e, pgx.ErrNoRows) {
			continue
		}
		if e != nil {
			return view, e
		}
		view.Events = append(view.Events, event)
	}
	return view, nil
}

func Prepare(ctx context.Context, pool *pgxpool.Pool, actorID int64, input PrepareInput) ([]Event, error) {
	if input.WarehouseID <= 0 || len(input.Events) == 0 || len(input.Events) > 50 {
		return nil, ErrInput
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var warehouseExists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM warehouses WHERE id=$1 AND is_active<>0)`, input.WarehouseID).Scan(&warehouseExists); err != nil {
		return nil, err
	}
	if !warehouseExists {
		return nil, ErrInput
	}
	// Stable lock order prevents concurrent overlapping multi-event selections from deadlocking.
	selections := append([]Selection(nil), input.Events...)
	sortSelections(selections)
	result := []Event{}
	for _, s := range selections {
		e, err := Enqueue(ctx, tx, s.OrderID, s.ShipmentOfferID, "manual")
		if err != nil {
			return nil, err
		}
		if e.IntentID == 0 || !e.CanPrepare {
			return nil, ErrNotReady
		}
		if s.Fingerprint == "" || s.Fingerprint != e.Fingerprint {
			return nil, ErrConflict
		}
		if e.State == "prepared" {
			if e.WarehouseID != input.WarehouseID {
				return nil, ErrConflict
			}
			if err = ensurePublication(ctx, tx, &e, input.WarehouseID, true); err != nil {
				return nil, err
			}
			result = append(result, e)
			continue
		}
		e.State = "prepared"
		e.WarehouseID = input.WarehouseID
		raw, err := json.Marshal(e)
		if err != nil {
			return nil, err
		}
		command, err := tx.Exec(ctx, `UPDATE order_sale_intents SET state='prepared',warehouse_id=$2,snapshot=$3,prepared_by=$4,prepared_at=CURRENT_TIMESTAMP WHERE id=$1 AND state='queued'`, e.IntentID, input.WarehouseID, raw, actorID)
		if err != nil {
			return nil, err
		}
		if command.RowsAffected() != 1 {
			return nil, ErrConflict
		}
		if err = tx.QueryRow(ctx, `SELECT prepared_at FROM order_sale_intents WHERE id=$1`, e.IntentID).Scan(&e.PreparedAt); err != nil {
			return nil, err
		}
		finish(&e)
		if err = ensurePublication(ctx, tx, &e, input.WarehouseID, true); err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func sortSelections(items []Selection) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].OrderID != items[j].OrderID {
			return items[i].OrderID < items[j].OrderID
		}
		return items[i].ShipmentOfferID < items[j].ShipmentOfferID
	})
}
