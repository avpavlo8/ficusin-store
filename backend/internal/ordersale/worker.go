package ordersale

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"
	"time"
)

// PublicationStep leases one bounded stage. Leases commit before calling the
// provider; the token prevents a late reply from overwriting recovery state.
func PublicationStep(ctx context.Context, pool *pgxpool.Pool, provider Provider, mode string) (bool, error) {
	if provider == nil || (mode != "mock" && mode != "live") {
		return false, nil
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	control, err := readDispatchControl(ctx, tx, true)
	if err != nil {
		return false, err
	}
	if !control.Enabled || control.Mode != mode {
		return false, nil
	}
	var id, intentID int64
	var status, hash string
	var raw, stateRaw []byte
	err = tx.QueryRow(ctx, `SELECT id,intent_id,status,request,request_hash,progress FROM order_sale_publications WHERE execution_mode=$1 AND ((status='queued' AND next_attempt_at<=NOW()) OR (status='processing' AND lease_until<NOW())) ORDER BY next_attempt_at,id FOR UPDATE SKIP LOCKED LIMIT 1`, mode).Scan(&id, &intentID, &status, &raw, &hash, &stateRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var request Request
	var progress Progress
	if err = json.Unmarshal(raw, &request); err != nil {
		return true, markInvalidClaim(ctx, tx, id, "Сохранённый запрос реализации повреждён")
	}
	actual, err := requestHash(request)
	if err != nil {
		return true, err
	}
	if actual != hash || request.ExecutionMode != mode || request.IntentID != intentID {
		return true, markInvalidClaim(ctx, tx, id, "Подтверждение сохранённого запроса не совпало; запрос в СБИС не отправлен")
	}
	if mode == "live" && request.BuyerID <= 0 {
		return true, markInvalidClaim(ctx, tx, id, "Не указан покупатель исходной реализации; требуется ручная сверка")
	}
	if err = json.Unmarshal(stateRaw, &progress); err != nil {
		return true, markInvalidClaim(ctx, tx, id, "Сохранённый этап реализации повреждён")
	}
	if progress.IdentityConflict {
		return true, markInvalidClaim(ctx, tx, id, "Документ СБИС уже принадлежит другой реализации; требуется ручная сверка")
	}
	if progress.Stage == "" {
		return true, markInvalidClaim(ctx, tx, id, "Сохранённый этап реализации отсутствует")
	}
	if progress.Unknown && progress.DocumentID == "" {
		return true, markInvalidClaim(ctx, tx, id, ErrRetryUnsafe.Error())
	}
	if writeStage(progress.Stage) && (status == "processing" || progress.Unknown || (progress.Stage == "create" && progress.DocumentID != "")) {
		var safe bool
		progress, safe = recoverProgress(progress)
		if !safe {
			stateRaw, _ = json.Marshal(progress)
			_, err = tx.Exec(ctx, `UPDATE order_sale_publications SET status='needs_attention',message=$2,progress=$3,lease_token='',lease_until=NULL,updated_at=NOW() WHERE id=$1`, id, ErrRetryUnsafe.Error(), stateRaw)
			if err != nil {
				return true, err
			}
			return true, tx.Commit(ctx)
		}
	}
	if (progress.Stage == "fill" || progress.Stage == "post") && (progress.DocumentID == "" || progress.DocumentUUID == "") {
		return true, markInvalidClaim(ctx, tx, id, "Не указан созданный документ; запись и проведение не выполнялись")
	}
	if writeStage(progress.Stage) {
		current, readErr := readEvent(ctx, tx, request.Event.OrderID, request.Event.ShipmentOfferID)
		if readErr != nil {
			return true, readErr
		}
		var activeWarehouse bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM warehouses WHERE id=$1 AND is_active<>0)`, request.SiteWarehouseID).Scan(&activeWarehouse); err != nil {
			return true, err
		}
		if !current.CanPrepare || current.Fingerprint != request.Event.Fingerprint || !activeWarehouse {
			return true, markInvalidClaim(ctx, tx, id, "Состав, связь товара или склад изменились; обмен остановлен для сверки")
		}
	}
	token, err := newOperationKey()
	if err != nil {
		return true, err
	}
	stateRaw, err = json.Marshal(progress)
	if err != nil {
		return true, err
	}
	if _, err = tx.Exec(ctx, `UPDATE order_sale_publications SET status='processing',progress=$2,lease_token=$3,lease_until=NOW()+INTERVAL '3 minutes',attempts=attempts+1,updated_at=NOW() WHERE id=$1`, id, stateRaw, token); err != nil {
		return true, err
	}
	if err = tx.Commit(ctx); err != nil {
		return true, err
	}
	callCtx, cancel := context.WithTimeout(ctx, 100*time.Second)
	defer cancel()
	result, callErr := provider.Step(callCtx, request, progress)
	if result.Progress.Stage == "" {
		result.Progress = progress
	}
	if callErr != nil {
		result.State = "failed"
		result.Message = callErr.Error()
		if writeStage(progress.Stage) {
			result.Progress.Unknown = true
			// A receipt returned alongside an error may still carry the known native ID.
			result.Progress.Stage = progress.Stage
			var safe bool
			result.Progress, safe = recoverProgress(result.Progress)
			if safe {
				result.State = "queued"
			} else {
				result.State = "needs_attention"
				result.Message = ErrRetryUnsafe.Error()
			}
		}
	}
	if callErr == nil && writeStage(progress.Stage) && result.State == "queued" && result.Progress.Stage == progress.Stage {
		// An acknowledged write which did not advance is still not permission
		// to send it again. Confirm its result through the read-only path.
		var safe bool
		result.Progress, safe = recoverProgress(result.Progress)
		if safe {
			result.Message = "Повторная запись не выполнялась; проверяем результат этапа"
		} else {
			result.State = "needs_attention"
			result.Message = ErrRetryUnsafe.Error()
		}
	}
	if progress.DocumentID != "" && (result.Progress.DocumentID != progress.DocumentID || (progress.DocumentUUID != "" && result.Progress.DocumentUUID != progress.DocumentUUID)) {
		result = Result{Progress: progress, State: "needs_attention", Message: "Идентификатор реализации изменился; обмен остановлен для сверки"}
	}
	if result.State != "queued" && result.State != "posted" && result.State != "needs_attention" && result.State != "failed" {
		result = Result{Progress: progress, State: "needs_attention", Message: "Неизвестный результат обмена; повторная запись не выполнялась"}
	}
	if result.State == "posted" && !validStockProof(request, result) {
		result.State = "needs_attention"
		result.Message = "Нет подтверждения проведения и складского движения; списание не подтверждено"
		result.StockVerified = false
		result.StockProof = nil
	}
	if result.State != "posted" {
		result.StockVerified = false
		result.StockProof = nil
	}
	if len([]rune(result.Message)) > 1200 {
		result.Message = string([]rune(result.Message)[:1200])
	}
	stateRaw, err = json.Marshal(result.Progress)
	if err != nil {
		return true, err
	}
	proof := result.StockProof
	if len(proof) == 0 {
		proof = json.RawMessage(`{}`)
	}
	delay := time.Duration(0)
	if callErr != nil && result.State == "queued" {
		delay = 10 * time.Second
	}
	if result.State == "queued" && result.RetryAfter > delay {
		delay = min(result.RetryAfter, 5*time.Minute)
	}
	command, err := pool.Exec(ctx, `UPDATE order_sale_publications SET status=$3,message=$4,progress=$5,document_id=$6,document_uuid=$7,stock_verified=$8,stock_proof=$9,lease_token='',lease_until=NULL,next_attempt_at=NOW()+$10::INTERVAL,updated_at=NOW() WHERE id=$1 AND lease_token=$2 AND status='processing'`, id, token, result.State, result.Message, stateRaw, result.Progress.DocumentID, result.Progress.DocumentUUID, result.StockVerified, proof, delay.String())
	if err != nil {
		var conflict *pgconn.PgError
		if errors.As(err, &conflict) && conflict.Code == "23505" && (conflict.ConstraintName == "order_sale_publications_document_id_unique" || conflict.ConstraintName == "order_sale_publications_document_uuid_unique") {
			// Retain the second vendor receipt for audit, but never claim the
			// document already owned by another handover or erase its proof.
			result.Progress.IdentityConflict = true
			result.Progress.ConflictingStockProof = result.StockProof
			stateRaw, encodeErr := json.Marshal(result.Progress)
			if encodeErr != nil {
				return true, encodeErr
			}
			command, err = pool.Exec(ctx, `UPDATE order_sale_publications SET status='needs_attention',message='Документ СБИС уже связан с другой реализацией; требуется сверка',progress=$3,stock_verified=FALSE,stock_proof='{}',lease_token='',lease_until=NULL,updated_at=NOW() WHERE id=$1 AND lease_token=$2 AND status='processing'`, id, token, stateRaw)
		}
		if err != nil {
			return true, err
		}
	}
	if command.RowsAffected() != 1 {
		return true, fmt.Errorf("realization lease changed; result retained for read-only recovery")
	}
	return true, nil
}
func markInvalidClaim(ctx context.Context, tx pgx.Tx, id int64, message string) error {
	if _, err := tx.Exec(ctx, `UPDATE order_sale_publications SET status='needs_attention',message=$2,lease_token='',lease_until=NULL,updated_at=NOW() WHERE id=$1`, id, message); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func RunPublisher(ctx context.Context, pool *pgxpool.Pool, provider Provider, mode string, logger *slog.Logger) {
	for ctx.Err() == nil {
		_, err := PublicationStep(ctx, pool, provider, mode)
		if err != nil {
			logger.Error("order realization publication step failed", "error", err)
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
