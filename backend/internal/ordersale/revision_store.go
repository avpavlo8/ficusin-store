package ordersale

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/avpavlo8/ficusin-store/backend/internal/money"
	"github.com/jackc/pgx/v5"
	"time"
)

func revisionLock(ctx context.Context, tx pgx.Tx, orderID int64) error {
	if _, e := tx.Exec(ctx, `SELECT id FROM orders WHERE id=$1 FOR UPDATE`, orderID); e != nil {
		return e
	}
	_, e := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('order-sale:'||$1::BIGINT::TEXT,0))`, orderID)
	return e
}
func revisionContentHash(e Event) (string, error) {
	raw, err := json.Marshal(struct {
		Key   string
		Lines []Line
	}{e.Key, e.Lines})
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:]), nil
}
func revisionLiveEvent(ctx context.Context, q queryer, orderID int64, base Event) (Event, json.RawMessage, error) {
	e := base
	e.Publication = nil
	e.Lines = []Line{}
	e.MappingConflict = false
	e.QuantityConflict = false
	e.CancellationRequested = false
	e.CancellationBlocked = false
	var audit json.RawMessage
	var net int64
	err := q.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(oi) ORDER BY oi.id),'[]'::JSONB) FROM order_items oi WHERE oi.order_id=$1`, orderID).Scan(&audit)
	if err != nil {
		return e, nil, err
	}
	rows, err := q.Query(ctx, `SELECT oi.id,COALESCE(oi.variant_id,0),oi.product_name,COALESCE(oi.sku,''),COALESCE((SELECT CASE WHEN COUNT(DISTINCT external_id)=1 THEN MIN(external_id) ELSE '' END FROM product_external_ids WHERE variant_id=oi.variant_id AND provider='saby' AND id_type='id' AND status='active' HAVING COUNT(*)>0),(SELECT saby_id FROM product_variants WHERE id=oi.variant_id),''),oi.quantity,oi.unit_price::TEXT FROM order_items oi WHERE order_id=$1 ORDER BY oi.id`, orderID)
	if err != nil {
		return e, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var l Line
		if err = rows.Scan(&l.OrderItemID, &l.VariantID, &l.Name, &l.SKU, &l.SabyID, &l.Quantity, &l.UnitPrice); err != nil {
			return e, nil, err
		}
		e.Lines = append(e.Lines, l)
	}
	if err = rows.Err(); err != nil {
		return e, nil, err
	}
	rows.Close()
	if err = q.QueryRow(ctx, `SELECT ROUND(COALESCE(o.subtotal,(SELECT SUM(unit_price*quantity) FROM order_items WHERE order_id=$1),0)*100)::BIGINT FROM orders o WHERE id=$1`, orderID).Scan(&net); err != nil {
		return e, nil, err
	}
	e.Lines, err = netRevisionLines(e.Lines, net)
	if err != nil {
		return e, nil, err
	}
	finish(&e)
	return e, audit, nil
}

// BeforeOrderEdit preserves full historical rows before any live item deletion.
// The original publisher request is never regenerated from edited contents.
func BeforeOrderEdit(ctx context.Context, tx pgx.Tx, orderID int64) error {
	if err := revisionLock(ctx, tx, orderID); err != nil {
		return err
	}
	var intent, baseID int64
	var snapshot, config []byte
	var warehouse int64
	err := tx.QueryRow(ctx, `SELECT id,COALESCE(base_revision_id,0),snapshot,COALESCE(warehouse_id,0),COALESCE(origin_config,'null') FROM order_sale_intents WHERE order_id=$1 AND event_trigger='created' FOR UPDATE`, orderID).Scan(&intent, &baseID, &snapshot, &warehouse, &config)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil || baseID > 0 {
		return err
	}
	var event Event
	if json.Unmarshal(snapshot, &event) != nil {
		return ErrConflict
	}
	event.IntentID = intent
	event.Trigger = "created"
	event.WarehouseID = warehouse
	finish(&event)
	var origin Request
	var raw []byte
	var posted bool
	var proof json.RawMessage
	var at *time.Time
	err = tx.QueryRow(ctx, `SELECT request,stock_verified,stock_proof,stock_verified_at FROM order_sale_publications WHERE intent_id=$1`, intent).Scan(&raw, &posted, &proof, &at)
	if err == nil {
		if json.Unmarshal(raw, &origin) != nil {
			return ErrConflict
		}
	} else if errors.Is(err, pgx.ErrNoRows) {
		var c DispatchControl
		if len(config) > 0 && string(config) != "null" {
			if json.Unmarshal(config, &c) != nil {
				return ErrConflict
			}
		} else {
			c, err = readDispatchControl(ctx, tx, true)
			if err != nil {
				return err
			}
		}
		origin = Request{IntentID: intent, Event: event, SiteWarehouseID: warehouse, NativeWarehouseID: c.NativeWarehouseID, MovementWarehouseID: c.MovementWarehouseID, OrganizationID: c.OrganizationID, RegulationID: c.RegulationID, BuyerID: c.BuyerID, ExecutionMode: c.Mode}
	} else {
		return err
	}
	_, audit, err := revisionLiveEvent(ctx, tx, orderID, event)
	if err != nil {
		return err
	}
	id, err := insertRevision(ctx, tx, intent, 1, origin, audit, "origin", false)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE order_sale_intents SET base_revision_id=$2::BIGINT,desired_revision_id=$2::BIGINT,applied_revision_id=CASE WHEN $3::BOOLEAN THEN $2::BIGINT ELSE NULL END WHERE id=$1`, intent, id, posted); err != nil {
		return err
	}
	if err = admitRevisionLayers(ctx, tx, intent, id, origin.Event.Lines, true); err != nil {
		return err
	}
	if posted && at != nil {
		if err = recordRevisionEffect(ctx, tx, intent, id, "origin_post", true, proof, *at, origin.Event.Lines); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE order_sale_revisions SET state='applied',post_verified=TRUE,stock_proof=$2,stock_verified_at=$3,applied_at=$3 WHERE id=$1`, id, proof, *at)
	}
	return err
}

// QueueRevision freezes the final canonical net plan in the same edit transaction.
func QueueRevision(ctx context.Context, tx pgx.Tx, orderID int64) error {
	if err := BeforeOrderEdit(ctx, tx, orderID); err != nil {
		return err
	}
	var intent, baseID, desiredID int64
	var snapshot []byte
	err := tx.QueryRow(ctx, `SELECT id,COALESCE(base_revision_id,0),COALESCE(desired_revision_id,0),snapshot FROM order_sale_intents WHERE order_id=$1 AND event_trigger='created' FOR UPDATE`, orderID).Scan(&intent, &baseID, &desiredID, &snapshot)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var origin Request
	var desiredHash string
	var raw []byte
	if err = tx.QueryRow(ctx, `SELECT request FROM order_sale_revisions WHERE id=$1`, baseID).Scan(&raw); err != nil {
		return err
	}
	if json.Unmarshal(raw, &origin) != nil {
		return ErrConflict
	}
	if err = tx.QueryRow(ctx, `SELECT content_hash FROM order_sale_revisions WHERE id=$1`, desiredID).Scan(&desiredHash); err != nil {
		return err
	}
	event, audit, err := revisionLiveEvent(ctx, tx, orderID, origin.Event)
	if err != nil {
		return err
	}
	event.IntentID = intent
	event.Trigger = "created"
	finish(&event)
	content, err := revisionContentHash(event)
	if err != nil {
		return err
	}
	if content == desiredHash {
		return nil
	}
	var number int64
	if err = tx.QueryRow(ctx, `SELECT COALESCE(MAX(revision_number),0)+1 FROM order_sale_revisions WHERE intent_id=$1`, intent).Scan(&number); err != nil {
		return err
	}
	target := origin
	target.Event = event
	id, err := insertRevision(ctx, tx, intent, number, target, audit, "queued", true)
	if err != nil {
		return err
	}
	if err = admitRevisionLayers(ctx, tx, intent, id, event.Lines, false); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE order_sale_revisions SET state='superseded' WHERE id=$1 AND state='queued' AND NOT EXISTS(SELECT 1 FROM order_sale_intents WHERE active_revision_id=$1)`, desiredID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE order_sale_intents SET desired_revision_id=$2 WHERE id=$1`, intent, id); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE order_sale_publications SET status=CASE WHEN status='processing' THEN status ELSE 'queued' END,message='Изменения заказа ожидают обновления той же реализации СБИС',next_attempt_at=NOW(),updated_at=NOW() WHERE intent_id=$1 AND desired_state='posted'`, intent)
	return err
}
func insertRevision(ctx context.Context, tx pgx.Tx, intent, number int64, request Request, audit json.RawMessage, state string, setID bool) (int64, error) {
	var id int64
	if err := tx.QueryRow(ctx, `SELECT nextval(pg_get_serial_sequence('order_sale_revisions','id'))`).Scan(&id); err != nil {
		return 0, err
	}
	if setID {
		request.RevisionID = id
	}
	hash, err := requestHash(request)
	if err != nil {
		return 0, err
	}
	content, err := revisionContentHash(request.Event)
	if err != nil {
		return 0, err
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO order_sale_revisions(id,intent_id,revision_number,request,request_hash,content_hash,order_items_snapshot,state)VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, id, intent, number, raw, hash, content, audit, state); err != nil {
		return 0, err
	}
	var snapshots []map[string]json.RawMessage
	if json.Unmarshal(audit, &snapshots) != nil {
		return 0, ErrConflict
	}
	byID := map[int64]json.RawMessage{}
	for _, row := range snapshots {
		var itemID int64
		json.Unmarshal(row["id"], &itemID)
		r, _ := json.Marshal(row)
		byID[itemID] = r
	}
	for _, line := range request.Event.Lines {
		if _, err = money.Parse(line.UnitPrice); err != nil {
			return 0, err
		}
		row := byID[line.OrderItemID]
		if len(row) == 0 {
			row = json.RawMessage(`{}`)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO order_sale_revision_lines(revision_id,order_item_id,variant_id,name,sku,saby_id,quantity,unit_price,line_snapshot)VALUES($1,$2,$3,$4,$5,$6,$7,$8::NUMERIC,$9)`, id, line.OrderItemID, line.VariantID, line.Name, line.SKU, line.SabyID, line.Quantity, line.UnitPrice, row); err != nil {
			return 0, err
		}
	}
	return id, nil
}
func admitRevisionLayers(ctx context.Context, tx pgx.Tx, intent, revision int64, lines []Line, origin bool) error {
	if _, err := tx.Exec(ctx, `SELECT order_sale_refresh_hold_releases($1)`, intent); err != nil {
		return err
	}
	quantities := map[int64]int64{}
	for _, l := range lines {
		if l.VariantID > 0 {
			quantities[l.VariantID] += int64(l.Quantity)
		}
	}
	for variant, qty := range quantities {
		var held int64
		if err := tx.QueryRow(ctx, `SELECT COALESCE(SUM(l.quantity-COALESCE((SELECT SUM(r.quantity)FROM order_sale_hold_releases r WHERE r.layer_id=l.id),0)),0) FROM order_sale_hold_layers l WHERE intent_id=$1 AND variant_id=$2`, intent, variant).Scan(&held); err != nil {
			return err
		}
		if qty > held {
			if _, err := tx.Exec(ctx, `INSERT INTO order_sale_hold_layers(intent_id,revision_id,variant_id,quantity,admitted_at) VALUES($1,$2,$3,$4,CASE WHEN $5 THEN (SELECT created_at FROM order_sale_intents WHERE id=$1) ELSE CURRENT_TIMESTAMP END)`, intent, revision, variant, qty-held, origin); err != nil {
				return err
			}
		}
	}
	return nil
}
func recordRevisionEffect(ctx context.Context, tx pgx.Tx, intent, revision int64, operation string, posted bool, proof json.RawMessage, at time.Time, lines []Line) error {
	var effect int64
	err := tx.QueryRow(ctx, `INSERT INTO order_sale_revision_effects(intent_id,revision_id,operation,posted,proof,verified_at)VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(revision_id,operation) DO UPDATE SET operation=EXCLUDED.operation RETURNING id`, intent, revision, operation, posted, proof, at).Scan(&effect)
	if err != nil {
		return err
	}
	qty := map[int64]int64{}
	for _, l := range lines {
		if l.VariantID > 0 {
			qty[l.VariantID] += int64(l.Quantity)
		}
	}
	for variant, q := range qty {
		if _, err = tx.Exec(ctx, `INSERT INTO order_sale_revision_effect_lines(effect_id,variant_id,quantity)VALUES($1,$2,$3)ON CONFLICT DO NOTHING`, effect, variant, q); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `SELECT order_sale_refresh_hold_releases($1)`, intent)
	return err
}

func loadRevisionViews(ctx context.Context, q queryer, intentID int64, view *PublicationView) error {
	var base, desired, active, applied int64
	if err := q.QueryRow(ctx, `SELECT COALESCE(base_revision_id,0),COALESCE(desired_revision_id,0),COALESCE(active_revision_id,0),COALESCE(applied_revision_id,0) FROM order_sale_intents WHERE id=$1`, intentID).Scan(&base, &desired, &active, &applied); err != nil {
		return err
	}
	load := func(id int64) (*RevisionView, error) {
		if id == 0 {
			return nil, nil
		}
		var v RevisionView
		err := q.QueryRow(ctx, `SELECT id,revision_number,state,created_at,applied_at FROM order_sale_revisions WHERE id=$1 AND intent_id=$2`, id, intentID).Scan(&v.ID, &v.Number, &v.State, &v.CreatedAt, &v.AppliedAt)
		return &v, err
	}
	var err error
	view.DesiredRevision, err = load(desired)
	if err != nil {
		return err
	}
	view.ActiveRevision, err = load(active)
	if err != nil {
		return err
	}
	view.AppliedRevision, err = load(applied)
	if err != nil {
		return err
	}
	view.RevisionPending = desired > 0 && desired != base && (desired != applied || active > 0)
	return nil
}

// RevisionPending guards assembly/payment/dispatch against an old native plan.
// Initial publication is not an edit revision and keeps its existing workflow.
func RevisionPending(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, orderID int64) (bool, error) {
	var pending bool
	err := q.QueryRow(ctx, `SELECT order_sale_revision_pending($1)`, orderID).Scan(&pending)
	return pending, err
}

type admissionUnknownError struct{}

func (admissionUnknownError) Error() string {
	return "Не подтверждён исходный режим обмена этого старого заказа. Нужна проверка владельца перед созданием реализации"
}
func (admissionUnknownError) Unwrap() error { return ErrNotReady }

type admissionModeError struct{}

func (admissionModeError) Error() string {
	return "Сохранённый режим обмена заказа отличается от текущего. Восстановите исходный режим; автоматический перевод на другую систему запрещён"
}
func (admissionModeError) Unwrap() error { return ErrNotReady }
