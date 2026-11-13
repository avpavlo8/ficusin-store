package ordersale

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
)

// EnqueueCreated must run inside the new-order transaction, after all items.
// Enabling the dispatcher never scans or promotes existing orders.
func EnqueueCreated(ctx context.Context, tx pgx.Tx, orderID int64) (Event, error) {
	c, err := readDispatchControl(ctx, tx, true)
	if err != nil {
		return Event{}, err
	}
	if !c.Enabled || c.Mode == "disabled" {
		return Event{}, nil
	}
	if _, err = tx.Exec(ctx, `SELECT id FROM orders WHERE id=$1 FOR UPDATE`, orderID); err != nil {
		return Event{}, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('order-sale:'||$1::BIGINT::TEXT,0))`, orderID); err != nil {
		return Event{}, err
	}
	e, err := readEvent(ctx, tx, orderID, 0)
	if err != nil {
		return e, err
	}
	if e.IntentID > 0 {
		return e, nil
	}
	if e.Status == "cancelled" {
		return e, nil
	}
	e.Trigger = "created"
	e.Source = "automatic"
	e.State = "queued"
	e.WarehouseID = c.SiteWarehouseID
	finish(&e)
	raw, err := json.Marshal(e)
	if err != nil {
		return e, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO order_sale_intents(event_key,order_id,source,snapshot,event_trigger,warehouse_id) VALUES($1,$2,'automatic',$3,'created',NULLIF($4,0)) RETURNING id`, e.Key, orderID, raw, c.SiteWarehouseID).Scan(&e.IntentID)
	if err != nil {
		return e, err
	}
	for _, l := range e.Lines {
		if _, err = tx.Exec(ctx, `INSERT INTO order_sale_intent_lines(intent_id,order_item_id,quantity,variant_id) VALUES($1,$2,$3,$4)`, e.IntentID, l.OrderItemID, l.Quantity, l.VariantID); err != nil {
			return e, err
		}
	}
	if err = ensurePublication(ctx, tx, &e, c.SiteWarehouseID, false); err != nil {
		return e, err
	}
	return e, nil
}

// RequestCancellation records a desired state only. It shares the transaction
// with cancellation and must run before overwriting the prior physical status.
func RequestCancellation(ctx context.Context, tx pgx.Tx, orderID int64) error {
	if _, err := tx.Exec(ctx, `SELECT id FROM orders WHERE id=$1 FOR UPDATE`, orderID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('order-sale:'||$1::BIGINT::TEXT,0))`, orderID); err != nil {
		return err
	}
	var intentID int64
	err := tx.QueryRow(ctx, `SELECT id FROM order_sale_intents WHERE order_id=$1 AND event_trigger='created' FOR UPDATE`, orderID).Scan(&intentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var physical bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(
 SELECT 1 FROM orders o WHERE o.id=$1 AND (o.status IN ('shipped','completed') OR UPPER(COALESCE(o.cdek_status,''))=ANY($2::TEXT[]))
 UNION ALL SELECT 1 FROM shipment_offers s WHERE s.order_id=$1 AND (s.status IN ('shipped','completed') OR UPPER(COALESCE(s.cdek_status,''))=ANY($2::TEXT[])))`, orderID, physicalCarrierStatuses).Scan(&physical)
	if err != nil {
		return err
	}
	// A leased in-flight writer keeps its token/result until it settles. Its next
	// claim sees desired_state and can only reconcile/cancel, never create/fill/post.
	_, err = tx.Exec(ctx, `UPDATE order_sale_intents SET cancellation_requested_at=COALESCE(cancellation_requested_at,NOW()),cancellation_blocked=cancellation_blocked OR $2 WHERE id=$1`, intentID, physical)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE order_sale_publications SET desired_state='cancelled',cancellation_blocked=cancellation_blocked OR $2,status=CASE WHEN status='processing' THEN status WHEN $2 THEN 'needs_attention' ELSE 'queued' END,message=CASE WHEN $2 THEN 'Товар уже передан; требуется проверка физического возврата' ELSE 'Запрошена отмена реализации' END,next_attempt_at=NOW(),updated_at=NOW() WHERE intent_id=$1 AND status<>'cancelled'`, intentID, physical)
	if err != nil {
		return err
	}
	carrier, err := carrierCancellationState(ctx, tx, orderID)
	if err != nil {
		return err
	}
	if carrier != "clear" {
		_, err = tx.Exec(ctx, `UPDATE order_sale_intents i SET cancellation_blocked=TRUE WHERE id=$1 AND NOT EXISTS(SELECT 1 FROM order_sale_publications p WHERE p.intent_id=i.id)`, intentID)
		return err
	}
	// No publication means no remote writer could ever have been claimed.
	_, err = tx.Exec(ctx, `UPDATE order_sale_intents i SET cancellation_verified=TRUE,cancellation_verified_at=COALESCE(cancellation_verified_at,NOW()) WHERE i.id=$1 AND NOT $2 AND NOT EXISTS(SELECT 1 FROM order_sale_publications p WHERE p.intent_id=i.id)`, intentID, physical)
	return err
}

var physicalCarrierStatuses = []string{"RECEIVED_AT_SHIPMENT_WAREHOUSE", "READY_FOR_SHIPMENT_IN_SENDER_CITY", "TAKEN_BY_TRANSPORTER_FROM_SENDER_CITY", "SENT_TO_RECIPIENT_CITY", "IN_TRANSIT", "ACCEPTED_IN_TRANSIT_CITY", "ACCEPTED_AT_TRANSIT_WAREHOUSE", "RECEIVED_AT_DELIVERY_WAREHOUSE", "ACCEPTED_AT_PICK_UP_POINT", "POSTOMAT_POSTED", "READY_FOR_RECIPIENT", "DELIVERED"}
