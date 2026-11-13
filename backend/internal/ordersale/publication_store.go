package ordersale

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RefreshDispatchControl mirrors validated application configuration, not a
// user-editable switch. It never inserts jobs for existing intents.
func RefreshDispatchControl(ctx context.Context, pool *pgxpool.Pool, c DispatchControl) error {
	if c.Mode != "disabled" && c.Mode != "mock" && c.Mode != "live" {
		return ErrInput
	}
	if c.Enabled && (c.Mode == "disabled" || (c.Mode == "live" && (c.BuyerID <= 0 || c.SiteWarehouseID <= 0 || c.NativeWarehouseID <= 0 || c.MovementWarehouseID <= 0 || c.OrganizationID <= 0 || c.RegulationID <= 0))) {
		return ErrInput
	}
	if c.Enabled && c.SiteWarehouseID > 0 {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM warehouses WHERE id=$1 AND is_active<>0)`, c.SiteWarehouseID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrInput
		}
	}
	command, err := pool.Exec(ctx, `UPDATE order_sale_dispatch_control SET execution_mode=$1,enabled=$2,site_warehouse_id=$3,native_warehouse_id=$4,organization_id=$5,regulation_id=$6,movement_warehouse_id=$7,buyer_id=$8,updated_at=NOW() WHERE singleton`, c.Mode, c.Enabled, c.SiteWarehouseID, c.NativeWarehouseID, c.OrganizationID, c.RegulationID, c.MovementWarehouseID, c.BuyerID)
	if err == nil && command.RowsAffected() != 1 {
		return ErrInput
	}
	return err
}
func readDispatchControl(ctx context.Context, q queryer, lock bool) (DispatchControl, error) {
	var c DispatchControl
	sql := `SELECT execution_mode,enabled,site_warehouse_id,native_warehouse_id,organization_id,regulation_id,movement_warehouse_id,buyer_id FROM order_sale_dispatch_control WHERE singleton`
	if lock {
		sql += " FOR SHARE"
	}
	err := q.QueryRow(ctx, sql).Scan(&c.Mode, &c.Enabled, &c.SiteWarehouseID, &c.NativeWarehouseID, &c.OrganizationID, &c.RegulationID, &c.MovementWarehouseID, &c.BuyerID)
	return c, err
}
func loadPublication(ctx context.Context, q queryer, intentID int64) (*PublicationView, error) {
	var v PublicationView
	var raw []byte
	var enabled bool
	var mode string
	err := q.QueryRow(ctx, `SELECT p.id,p.execution_mode,p.status,p.message,p.document_id,p.document_uuid,p.attempts,p.updated_at,p.progress,c.enabled,c.execution_mode,(p.request->>'siteWarehouseId')::BIGINT,p.stock_verified FROM order_sale_publications p CROSS JOIN order_sale_dispatch_control c WHERE p.intent_id=$1 AND c.singleton`, intentID).Scan(&v.ID, &v.ExecutionMode, &v.Status, &v.Message, &v.DocumentID, &v.DocumentUUID, &v.Attempts, &v.UpdatedAt, &raw, &enabled, &mode, &v.SiteWarehouseID, &v.StockVerified)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var progress Progress
	if err = json.Unmarshal(raw, &progress); err != nil {
		return nil, err
	}
	v.TestMode = v.ExecutionMode == "mock"
	v.CanRetry = enabled && mode == v.ExecutionMode && retrySafe(v.Status, progress)
	return &v, nil
}
func ensurePublication(ctx context.Context, tx pgx.Tx, e *Event, warehouseID int64, explicit bool) error {
	c, err := readDispatchControl(ctx, tx, true)
	if err != nil {
		return err
	}
	if !c.Enabled || c.Mode == "disabled" {
		return nil
	}
	existing, err := loadPublication(ctx, tx, e.IntentID)
	if err != nil {
		return err
	}
	if existing != nil {
		if explicit && existing.SiteWarehouseID != warehouseID {
			return ErrConflict
		}
		e.Publication = existing
		return nil
	}
	// Keep the intent visible but do not freeze an unusable request. After a
	// missing mapping is fixed, explicit preparation can create its first job.
	if !e.CanPrepare {
		return nil
	}
	if !explicit {
		warehouseID = c.SiteWarehouseID
	}
	// Automatic dispatch requires an explicit default site warehouse. A later
	// manual preparation may choose one, but enabling never drains old intents.
	if warehouseID <= 0 {
		return nil
	}
	if c.Mode == "live" && warehouseID != c.SiteWarehouseID {
		return ErrConflict
	}
	key, err := newOperationKey()
	if err != nil {
		return err
	}
	copy := *e
	copy.Publication = nil
	copy.WarehouseID = warehouseID
	request := Request{BuyerID: c.BuyerID, IntentID: e.IntentID, Event: copy, SiteWarehouseID: warehouseID, NativeWarehouseID: c.NativeWarehouseID, MovementWarehouseID: c.MovementWarehouseID, OrganizationID: c.OrganizationID, RegulationID: c.RegulationID, OperationKey: key, ExecutionMode: c.Mode}
	hash, err := requestHash(request)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO order_sale_publications(intent_id,execution_mode,request,request_hash) VALUES($1,$2,$3,$4) ON CONFLICT(intent_id) DO NOTHING`, e.IntentID, c.Mode, raw, hash); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE order_sale_intents SET warehouse_id=$2 WHERE id=$1 AND warehouse_id IS NULL`, e.IntentID, warehouseID); err != nil {
		return err
	}
	e.WarehouseID = warehouseID
	e.Publication, err = loadPublication(ctx, tx, e.IntentID)
	return err
}

func RetryPublication(ctx context.Context, pool *pgxpool.Pool, id int64) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Match worker lock order: control before publication, including startup refresh.
	c, err := readDispatchControl(ctx, tx, true)
	if err != nil {
		return err
	}
	var status, mode string
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT status,execution_mode,progress FROM order_sale_publications WHERE id=$1 FOR UPDATE`, id).Scan(&status, &mode, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInput
	}
	if err != nil {
		return err
	}
	if !c.Enabled || c.Mode != mode {
		return ErrRetryUnsafe
	}
	var p Progress
	if err = json.Unmarshal(raw, &p); err != nil {
		return err
	}
	if !retrySafe(status, p) {
		return ErrRetryUnsafe
	}
	if writeStage(p.Stage) {
		var safe bool
		p, safe = recoverProgress(p)
		if !safe {
			return ErrRetryUnsafe
		}
	}
	raw, err = json.Marshal(p)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE order_sale_publications SET status='queued',progress=$2,message='Повторная проверка состояния',lease_token='',lease_until=NULL,next_attempt_at=NOW(),updated_at=NOW() WHERE id=$1`, id, raw); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
