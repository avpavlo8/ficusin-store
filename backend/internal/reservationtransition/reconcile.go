// Package reservationtransition plans historical reservation cutover without
// guessing ownership. Its apply pilot is restricted to a disposable local DB.
package reservationtransition

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrBlocked = errors.New("reservation reconciliation blocked")
var ErrChanged = errors.New("reservation snapshot changed; review a new dry-run")
var ErrLocalOnly = errors.New("apply requires loopback disposable ficusin_legacy_reconcile_test")
var ErrOutcomeUnknown = errors.New("commit outcome unknown; inspect the same operation key before any new apply")

type Group struct {
	VariantID          int64           `json:"variantId"`
	Fingerprint        string          `json:"fingerprint"`
	InventoryReserved  int64           `json:"inventoryReserved"`
	UnreleasedReserved int64           `json:"unreleasedReserved"`
	OrderIDs           []int64         `json:"orderIds"`
	Blockers           []string        `json:"blockers"`
	Safe               bool            `json:"safe"`
	Snapshot           json.RawMessage `json:"-"`
}
type Report struct {
	DryRun                 bool           `json:"dryRun"`
	Complete               bool           `json:"complete"`
	Groups                 []Group        `json:"groups,omitempty"`
	SafeGroups             int            `json:"safeGroups"`
	BlockedGroups          int            `json:"blockedGroups"`
	UnassignedRows         int64          `json:"unassignedRows"`
	UnfinishedPayments     int64          `json:"unfinishedPayments"`
	PaymentLookupWithoutID int64          `json:"paymentLookupWithoutId"`
	Blockers               map[string]int `json:"blockers"`
	PaymentReads           *PaymentReads  `json:"paymentReads,omitempty"`
}
type Result struct {
	OperationKey         string `json:"operationKey"`
	AlreadyApplied       bool   `json:"alreadyApplied"`
	PhysicalStockChanged bool   `json:"physicalStockChanged"`
	ProviderWrites       bool   `json:"providerWrites"`
}
type queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// Audit uses a read-only repeatable snapshot. It never imports the payment
// service: even a waiting authorization cannot trigger Capture/Cancel/Create.
func Audit(ctx context.Context, pool *pgxpool.Pool, limit int) (Report, error) {
	r := Report{DryRun: true, Complete: true, Groups: []Group{}, Blockers: map[string]int{}}
	if limit < 1 || limit > 1000 {
		return r, errors.New("limit must be between 1 and 1000")
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return r, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT variant_id FROM (
 SELECT variant_id FROM inventory WHERE reserved_qty<>0
 UNION SELECT variant_id FROM order_items WHERE reserved_qty<>0
 UNION SELECT variant_id FROM shipment_offer_items WHERE checkout_reserved_qty<>0
 UNION SELECT i.variant_id FROM shipment_offer_inventory_reservations s JOIN inventory i ON i.id=s.inventory_id
 ) candidates WHERE variant_id IS NOT NULL ORDER BY variant_id LIMIT $1`, limit+1)
	if err != nil {
		return r, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return r, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return r, err
	}
	if len(ids) > limit {
		r.Complete = false
		ids = ids[:limit]
	}
	for _, id := range ids {
		g, e := readGroup(ctx, tx, id)
		if e != nil {
			return r, e
		}
		r.Groups = append(r.Groups, g)
		if g.Safe {
			r.SafeGroups++
		} else {
			r.BlockedGroups++
		}
		for _, b := range g.Blockers {
			r.Blockers[b]++
		}
	}
	err = tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM order_items WHERE reserved_qty<>0 AND variant_id IS NULL)+(SELECT count(*) FROM shipment_offer_items WHERE checkout_reserved_qty<>0 AND variant_id IS NULL),count(*) FILTER(WHERE status NOT IN('paid','cancelled','canceled','refunded')),count(*) FILTER(WHERE status NOT IN('paid','cancelled','canceled','refunded') AND provider_payment_id='') FROM payments`).Scan(&r.UnassignedRows, &r.UnfinishedPayments, &r.PaymentLookupWithoutID)
	if err != nil {
		return r, err
	}
	return r, tx.Commit(ctx)
}

func readGroup(ctx context.Context, q queryer, id int64) (Group, error) {
	g := Group{VariantID: id, OrderIDs: []int64{}, Blockers: []string{}}
	parts := map[string]json.RawMessage{}
	var raw []byte
	if err := q.QueryRow(ctx, `SELECT COALESCE(sum(reserved_qty),0),COALESCE(jsonb_agg(jsonb_build_object('id',id,'warehouseId',warehouse_id,'availableQty',available_qty,'reservedQty',reserved_qty,'syncedAt',synced_at,'observedAt',stock_observed_at) ORDER BY id),'[]'::jsonb) FROM inventory WHERE variant_id=$1`, id).Scan(&g.InventoryReserved, &raw); err != nil {
		return g, err
	}
	parts["inventory"] = raw
	var active, multi, carrier bool
	err := q.QueryRow(ctx, `SELECT COALESCE(sum(oi.reserved_qty) FILTER(WHERE o.stock_released_at IS NULL),0),
 COALESCE(array_agg(DISTINCT o.id ORDER BY o.id) FILTER(WHERE o.stock_released_at IS NULL),'{}'::bigint[]),
 COALESCE(bool_or(o.status<>'cancelled') FILTER(WHERE o.stock_released_at IS NULL),false),
 COALESCE(bool_or(EXISTS(SELECT 1 FROM order_items other WHERE other.order_id=o.id AND other.variant_id IS DISTINCT FROM $1 AND other.reserved_qty<>0)) FILTER(WHERE o.stock_released_at IS NULL),false),
 COALESCE(bool_or(o.cdek_create_state IN('creating','unknown','manual_review') OR (COALESCE(o.cdek_uuid,'')<>'' AND COALESCE(o.cdek_cancel_state,'')<>'cancelled' AND UPPER(COALESCE(o.cdek_status,''))<>'REMOVED')) FILTER(WHERE o.stock_released_at IS NULL),false),
 COALESCE(jsonb_agg(jsonb_build_object('itemId',oi.id,'orderId',o.id,'reservedQty',oi.reserved_qty,'quantity',oi.quantity,'status',o.status,'stockReleasedAt',o.stock_released_at,'carrierCreateState',o.cdek_create_state,'carrierCancelState',o.cdek_cancel_state,'carrierStatus',o.cdek_status,'carrierLinked',COALESCE(o.cdek_uuid,'')<>'') ORDER BY oi.id),'[]'::jsonb)
 FROM order_items oi JOIN orders o ON o.id=oi.order_id WHERE oi.variant_id=$1 AND oi.reserved_qty<>0`, id).Scan(&g.UnreleasedReserved, &g.OrderIDs, &active, &multi, &carrier, &raw)
	if err != nil {
		return g, err
	}
	parts["orderItems"] = raw
	block := func(flag bool, reason string) {
		if flag {
			g.Blockers = append(g.Blockers, reason)
		}
	}
	block(active, "active_order")
	block(multi, "multi_variant_ownership")
	block(carrier, "carrier_cancellation_unresolved")
	var payment bool
	var paymentRows int64
	err = q.QueryRow(ctx, `SELECT count(*),COALESCE(bool_or(status NOT IN('cancelled','canceled') OR (provider_payment_id='' AND request_payload IS NOT NULL)),false),COALESCE(jsonb_agg(jsonb_build_object('id',id,'orderId',order_id,'status',status,'amount',amount,'providerIdKnown',provider_payment_id<>'','providerIdHash',encode(sha256(convert_to(provider_payment_id,'UTF8')),'hex'),'createAttempts',create_attempts,'updatedAt',updated_at) ORDER BY id),'[]'::jsonb) FROM payments WHERE order_id=ANY($1::bigint[])`, g.OrderIDs).Scan(&paymentRows, &payment, &raw)
	if err != nil {
		return g, err
	}
	parts["payments"] = raw
	block(payment, "payment_outcome_unresolved")
	block(paymentRows > 0, "payment_readback_requires_separate_review")
	var offers bool
	err = q.QueryRow(ctx, `SELECT COALESCE(bool_or(s.cdek_create_state IN('creating','unknown','manual_review') OR s.status IN('paid','shipping','shipped','completed') OR (s.cdek_uuid<>'' AND UPPER(COALESCE(s.cdek_status,''))<>'REMOVED') OR EXISTS(SELECT 1 FROM shipment_offer_items si WHERE si.shipment_offer_id=s.id AND si.checkout_reserved_qty<>0)),false),COALESCE(jsonb_agg(jsonb_build_object('id',s.id,'status',s.status,'createState',s.cdek_create_state,'carrierStatus',s.cdek_status,'carrierLinked',s.cdek_uuid<>'','updatedAt',s.updated_at) ORDER BY s.id),'[]'::jsonb) FROM shipment_offers s WHERE s.order_id=ANY($1::bigint[])`, g.OrderIDs).Scan(&offers, &raw)
	if err != nil {
		return g, err
	}
	parts["offers"] = raw
	block(offers, "shipment_offer_unresolved")
	var offerRows int64
	err = q.QueryRow(ctx, `SELECT count(*),COALESCE(jsonb_agg(jsonb_build_object('offerItemId',r.shipment_offer_item_id,'inventoryId',r.inventory_id,'quantity',r.quantity) ORDER BY r.shipment_offer_item_id,r.inventory_id),'[]'::jsonb) FROM shipment_offer_inventory_reservations r JOIN inventory i ON i.id=r.inventory_id LEFT JOIN shipment_offer_items si ON si.id=r.shipment_offer_item_id WHERE i.variant_id=$1 OR si.variant_id=$1`, id).Scan(&offerRows, &raw)
	if err != nil {
		return g, err
	}
	parts["offerReservations"] = raw
	block(offerRows > 0, "offer_warehouse_attribution_requires_review")
	var ledger bool
	err = q.QueryRow(ctx, `SELECT COALESCE(bool_or(NOT i.cancellation_verified OR i.cancellation_blocked OR (p.id IS NOT NULL AND (NOT p.cancellation_verified OR p.status<>'cancelled'))),false),COALESCE(jsonb_agg(jsonb_build_object('intentId',i.id,'verified',i.cancellation_verified,'blocked',i.cancellation_blocked,'publicationId',p.id,'publicationStatus',p.status,'publicationVerified',p.cancellation_verified) ORDER BY i.id),'[]'::jsonb) FROM order_sale_intents i LEFT JOIN order_sale_publications p ON p.intent_id=i.id WHERE i.order_id=ANY($1::bigint[]) AND i.event_trigger='created'`, g.OrderIDs).Scan(&ledger, &raw)
	if err != nil {
		return g, err
	}
	parts["realization"] = raw
	block(ledger, "realization_cancellation_unresolved")
	var releases int64
	err = q.QueryRow(ctx, `SELECT count(*),COALESCE(jsonb_agg(jsonb_build_object('id',id,'orderId',order_id,'quantity',quantity,'status',status) ORDER BY id),'[]'::jsonb) FROM stock_movements WHERE order_id=ANY($1::bigint[]) AND variant_id=$2 AND kind='release'`, g.OrderIDs, id).Scan(&releases, &raw)
	if err != nil {
		return g, err
	}
	parts["releaseJournal"] = raw
	block(releases > 0, "existing_release_receipt_requires_review")
	block(g.InventoryReserved <= 0 || g.UnreleasedReserved <= 0, "no_unreleased_reservation")
	block(g.InventoryReserved != g.UnreleasedReserved, "reservation_quantity_mismatch")
	block(g.InventoryReserved > 1<<31-1 || g.UnreleasedReserved > 1<<31-1, "quantity_out_of_range")
	parts["variantId"], _ = json.Marshal(id)
	g.Snapshot, err = json.Marshal(parts)
	if err != nil {
		return g, err
	}
	sum := sha256.Sum256(g.Snapshot)
	g.Fingerprint = hex.EncodeToString(sum[:])
	g.Safe = len(g.Blockers) == 0
	return g, nil
}

// ApplyLocal releases only a complete, unchanged, cancelled single-variant
// ownership group. It never changes physical stock, payments or FIFO ledgers.
func ApplyLocal(ctx context.Context, pool *pgxpool.Pool, id int64, fingerprint string) (Result, error) {
	result := Result{}
	if id <= 0 || len(fingerprint) != 64 {
		return result, ErrChanged
	}
	for _, c := range fingerprint {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return result, ErrChanged
		}
	}
	var database, host string
	if err := pool.QueryRow(ctx, `SELECT current_database(),COALESCE(host(inet_server_addr()),'')`).Scan(&database, &host); err != nil {
		return result, err
	}
	if database != "ficusin_legacy_reconcile_test" || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return result, ErrLocalOnly
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("legacy-reservation-v1:%d:%s", id, fingerprint)))
	result.OperationKey = hex.EncodeToString(sum[:])
	tx, err := pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SET LOCAL lock_timeout='2s';SET LOCAL statement_timeout='15s'`); err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('legacy-reservation-reconcile:'||$1::bigint::text,0))`, id); err != nil {
		return result, err
	}
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM legacy_reservation_reconciliations WHERE operation_key=$1 AND variant_id=$2 AND fingerprint=$3)`, result.OperationKey, id, fingerprint).Scan(&exists); err != nil {
		return result, err
	}
	if exists {
		result.AlreadyApplied = true
		return result, nil
	}
	// Parent -> stock advisory -> inventory matches checkout/admin lock order.
	rows, err := tx.Query(ctx, `SELECT id FROM orders WHERE id IN(SELECT order_id FROM order_items WHERE variant_id=$1 AND reserved_qty<>0) ORDER BY id FOR UPDATE`, id)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var order int64
		if err = rows.Scan(&order); err != nil {
			rows.Close()
			return result, err
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('order-stock:'||$1::bigint::text,0))`, id); err != nil {
		return result, err
	}
	// Row/table mutation is local only; short shared locks also exclude legacy
	// producers that predate the stock advisory convention during the snapshot.
	if _, err = tx.Exec(ctx, `LOCK TABLE order_items,shipment_offers,shipment_offer_items,shipment_offer_inventory_reservations,payments,order_sale_intents,order_sale_publications,stock_movements IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, `SELECT id FROM inventory WHERE variant_id=$1 ORDER BY id FOR UPDATE`, id); err != nil {
		return result, err
	}
	g, err := readGroup(ctx, tx, id)
	if err != nil {
		return result, err
	}
	if g.Fingerprint != fingerprint {
		return result, ErrChanged
	}
	if !g.Safe {
		return result, ErrBlocked
	}
	if _, err = tx.Exec(ctx, `INSERT INTO stock_movements(order_id,variant_id,saby_id,kind,quantity,status,reason) SELECT oi.order_id,$1,COALESCE(v.saby_id,''),'release',SUM(oi.reserved_qty)::integer,'local_reconciled',$3 FROM order_items oi JOIN product_variants v ON v.id=oi.variant_id WHERE oi.variant_id=$1 AND oi.order_id=ANY($2::bigint[]) GROUP BY oi.order_id,v.saby_id`, id, g.OrderIDs, "legacy_reservation_reconciliation:"+result.OperationKey); err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, `UPDATE inventory SET reserved_qty=0 WHERE variant_id=$1 AND reserved_qty<>0`, id); err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, `UPDATE order_items SET reserved_qty=0 WHERE variant_id=$1 AND order_id=ANY($2::bigint[]) AND reserved_qty<>0`, id, g.OrderIDs); err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, `UPDATE orders SET stock_released_at=CURRENT_TIMESTAMP WHERE id=ANY($1::bigint[]) AND stock_released_at IS NULL`, g.OrderIDs); err != nil {
		return result, err
	}
	after, err := readGroup(ctx, tx, id)
	if err != nil {
		return result, err
	}
	// readGroup normally lists outstanding ownership only. Retain the matched
	// owner rows in the after receipt too, proving zero reservations alongside
	// their unchanged ordered quantities and identities.
	var afterItems json.RawMessage
	if err = tx.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object('itemId',oi.id,'orderId',o.id,'reservedQty',oi.reserved_qty,'quantity',oi.quantity,'status',o.status,'stockReleasedAt',o.stock_released_at) ORDER BY oi.id),'[]'::jsonb) FROM order_items oi JOIN orders o ON o.id=oi.order_id WHERE oi.variant_id=$1 AND oi.order_id=ANY($2::bigint[])`, id, g.OrderIDs).Scan(&afterItems); err != nil {
		return result, err
	}
	var afterParts map[string]json.RawMessage
	if err = json.Unmarshal(after.Snapshot, &afterParts); err != nil {
		return result, err
	}
	afterParts["orderItems"] = afterItems
	after.Snapshot, err = json.Marshal(afterParts)
	if err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO legacy_reservation_reconciliations(operation_key,variant_id,fingerprint,before_snapshot,after_snapshot) VALUES($1,$2,$3,$4,$5)`, result.OperationKey, id, fingerprint, g.Snapshot, after.Snapshot); err != nil {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, ErrOutcomeUnknown
	}
	return result, nil
}
