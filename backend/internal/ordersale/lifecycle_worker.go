package ordersale

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

func retainsOwnedRowIDs(before, after []int64) bool {
	ids := map[int64]bool{}
	for _, id := range after {
		if id <= 0 || ids[id] {
			return false
		}
		ids[id] = true
	}
	for _, id := range before {
		if !ids[id] {
			return false
		}
	}
	return true
}
func createdStockReady(ctx context.Context, q queryer, r Request) (bool, error) {
	quantities := map[int64]int64{}
	for _, line := range r.Event.Lines {
		if line.VariantID <= 0 || line.Quantity <= 0 {
			return false, nil
		}
		quantities[line.VariantID] += int64(line.Quantity)
	}
	joins := " JOIN warehouses w ON w.id=i.warehouse_id AND w.is_active=1 "
	if r.ExecutionMode == "live" {
		joins += " JOIN product_variants v ON v.id=i.variant_id JOIN products p ON p.id=v.product_id AND 'stock'=ANY(p.saby_fields) JOIN saby_nomenclature n ON n.saby_id=v.saby_id AND n.missing_since IS NULL AND w.saby_id='saby-ryazan-main' "
	}
	for variant, qty := range quantities {
		var available int64
		var fresh bool
		err := q.QueryRow(ctx, `SELECT COALESCE(SUM(i.available_qty-i.reserved_qty),0)+COALESCE(order_sale_owned_quantity($1,$2,$3),0),COALESCE(BOOL_AND(i.synced_at>=NOW()-INTERVAL '2 hours'),FALSE) FROM order_sale_inventory i`+joins+` WHERE i.warehouse_id=$2 AND i.variant_id=$3`, r.Event.OrderID, r.SiteWarehouseID, variant).Scan(&available, &fresh)
		if err != nil {
			return false, err
		}
		if !fresh || available < qty {
			return false, nil
		}
	}
	return len(quantities) > 0, nil
}
func persistPublicationResult(ctx context.Context, pool *pgxpool.Pool, id, intentID int64, token string, result Result, delay time.Duration) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Cancellation locks intent before publication. Match that order here so an
	// in-flight vendor reply and a simultaneous cancellation cannot deadlock.
	if _, err = tx.Exec(ctx, `SELECT id FROM order_sale_intents WHERE id=$1 FOR UPDATE`, intentID); err != nil {
		return err
	}
	var desired string
	var blocked, created, posted bool
	if err = tx.QueryRow(ctx, `SELECT desired_state,cancellation_blocked,create_attempted,post_attempted FROM order_sale_publications WHERE id=$1 AND lease_token=$2 AND status='processing' FOR UPDATE`, id, token).Scan(&desired, &blocked, &created, &posted); err != nil {
		return err
	}
	result.Progress.DesiredState = desired
	result.Progress.CreateAttempted = created
	result.Progress.PostAttempted = posted
	// Keep a positive posting receipt even if cancellation arrived while that
	// request was in flight. The next stage reverses it instead of losing proof.
	if desired == "cancelled" && result.State != "cancelled" && !result.Progress.IdentityConflict && !cancellationStage(result.Progress.Stage) {
		result.State = "queued"
		result.Progress.Stage = "inspect-cancel"
		result.Message = "Запрошена отмена; проверяем собственный документ без повторного создания"
	}
	if blocked {
		result.State = "needs_attention"
		result.CancellationVerified = false
		result.Message = "Товар уже передан; требуется проверка физического возврата"
	}
	raw, err := json.Marshal(result.Progress)
	if err != nil {
		return err
	}
	proof := result.StockProof
	if len(proof) == 0 {
		proof = json.RawMessage(`{}`)
	}
	cancelProof := result.CancellationProof
	if len(cancelProof) == 0 {
		cancelProof = json.RawMessage(`{}`)
	}
	command, err := tx.Exec(ctx, `UPDATE order_sale_publications SET status=$3,message=$4,progress=$5,document_id=$6,document_uuid=$7,
 stock_verified=stock_verified OR $8,stock_proof=CASE WHEN $8 THEN $9 ELSE stock_proof END,stock_verified_at=CASE WHEN $8 THEN COALESCE(stock_verified_at,NOW()) ELSE stock_verified_at END,
 cancellation_verified=cancellation_verified OR $11,cancellation_proof=CASE WHEN $11 THEN $12 ELSE cancellation_proof END,cancellation_verified_at=CASE WHEN $11 THEN COALESCE(cancellation_verified_at,NOW()) ELSE cancellation_verified_at END,
 lease_token='',lease_until=NULL,next_attempt_at=NOW()+$10::INTERVAL,updated_at=NOW() WHERE id=$1 AND lease_token=$2 AND status='processing'`, id, token, result.State, result.Message, raw, result.Progress.DocumentID, result.Progress.DocumentUUID, result.StockVerified, proof, delay.String(), result.CancellationVerified, cancelProof)
	if err != nil {
		var conflict *pgconn.PgError
		if errors.As(err, &conflict) && conflict.Code == "23505" && (conflict.ConstraintName == "order_sale_publications_document_id_unique" || conflict.ConstraintName == "order_sale_publications_document_uuid_unique") {
			// An aborted statement invalidates this transaction. Preserve the conflict
			// audit in a separate guarded update, never overwrite the first owner.
			tx.Rollback(ctx)
			result.Progress.IdentityConflict = true
			result.Progress.ConflictingStockProof = result.StockProof
			raw, _ = json.Marshal(result.Progress)
			command, err = pool.Exec(ctx, `UPDATE order_sale_publications SET status='needs_attention',message='Документ СБИС уже связан с другой реализацией; требуется сверка',progress=$3,lease_token='',lease_until=NULL,updated_at=NOW() WHERE id=$1 AND lease_token=$2 AND status='processing'`, id, token, raw)
			if err != nil {
				return err
			}
			if command.RowsAffected() != 1 {
				return fmt.Errorf("realization lease changed")
			}
			return nil
		}
		return err
	}
	if command.RowsAffected() != 1 {
		return fmt.Errorf("realization lease changed; read-only recovery required")
	}
	if result.CancellationVerified {
		if _, err = tx.Exec(ctx, `UPDATE order_sale_intents SET cancellation_verified=TRUE,cancellation_verified_at=COALESCE(cancellation_verified_at,NOW()) WHERE id=$1`, intentID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func cancellationStage(stage string) bool {
	switch stage {
	case "inspect-cancel", "discover-cancel", "cancel", "verify-cancel", "reconcile-cancel", "cancelled":
		return true
	}
	return false
}

// Carrier cancellation and Saby reversal are independent workers. Local CREATED
// is not evidence that the carrier has not already received the physical goods.
func carrierCancellationState(ctx context.Context, q queryer, orderID int64) (string, error) {
	var uuid, createState, cancelState, status string
	if err := q.QueryRow(ctx, `SELECT COALESCE(cdek_uuid,''),COALESCE(cdek_create_state,''),COALESCE(cdek_cancel_state,''),UPPER(COALESCE(cdek_status,'')) FROM orders WHERE id=$1`, orderID).Scan(&uuid, &createState, &cancelState, &status); err != nil {
		return "", err
	}
	var unsafeOffer bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM shipment_offers WHERE order_id=$1 AND (cdek_create_state IN ('creating','unknown') OR (delivery_method='cdek' AND status IN ('paid','shipping') AND UPPER(COALESCE(cdek_status,''))<>'REMOVED') OR (COALESCE(cdek_uuid,'')<>'' AND UPPER(COALESCE(cdek_status,''))<>'REMOVED')))`, orderID).Scan(&unsafeOffer); err != nil {
		return "", err
	}
	if unsafeOffer {
		return "manual", nil
	}
	if cancelState == "manual_review" && status != "REMOVED" {
		return "manual", nil
	}
	if createState == "creating" || createState == "unknown" {
		return "pending", nil
	}
	if status == "REMOVED" || cancelState == "cancelled" {
		return "clear", nil
	}
	if uuid != "" {
		return "pending", nil
	}
	return "clear", nil
}
