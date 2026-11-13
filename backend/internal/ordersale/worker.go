package ordersale

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
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
	var status, hash, desired string
	var createAttempted, postAttempted, cancellationBlocked bool
	var raw, stateRaw []byte
	err = tx.QueryRow(ctx, `SELECT id,intent_id,status,request,request_hash,progress,desired_state,create_attempted,post_attempted,cancellation_blocked FROM order_sale_publications WHERE execution_mode=$1 AND ((status='queued' AND next_attempt_at<=NOW()) OR (status='processing' AND lease_until<NOW())) ORDER BY next_attempt_at,id FOR UPDATE SKIP LOCKED LIMIT 1`, mode).Scan(&id, &intentID, &status, &raw, &hash, &stateRaw, &desired, &createAttempted, &postAttempted, &cancellationBlocked)
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
	progress.DesiredState = desired
	progress.CreateAttempted = createAttempted
	progress.PostAttempted = postAttempted
	if cancellationBlocked {
		return true, markInvalidClaim(ctx, tx, id, "Товар уже передан; требуется проверка физического возврата")
	}
	if desired == "cancelled" {
		carrier, carrierErr := carrierCancellationState(ctx, tx, request.Event.OrderID)
		if carrierErr != nil {
			return true, carrierErr
		}
		if carrier == "manual" {
			return true, markInvalidClaim(ctx, tx, id, "Отмена СДЭК не подтверждена; требуется ручная сверка доставки, остаток не возвращён")
		}
		if carrier == "pending" {
			if _, err = tx.Exec(ctx, `UPDATE order_sale_publications SET status='queued',message='Ожидаем подтверждение отмены СДЭК; остаток пока не возвращён',lease_token='',lease_until=NULL,next_attempt_at=NOW()+INTERVAL '30 seconds',updated_at=NOW() WHERE id=$1`, id); err != nil {
				return true, err
			}
			return true, tx.Commit(ctx)
		}
	}
	if desired == "cancelled" {
		if !createAttempted && progress.DocumentID == "" {
			progress.Stage = "cancelled"
			progress.Unknown = false
			stateRaw, _ = json.Marshal(progress)
			if _, err = tx.Exec(ctx, `UPDATE order_sale_publications SET status='cancelled',progress=$2,cancellation_verified=TRUE,cancellation_proof='{"kind":"no_remote"}',cancellation_verified_at=COALESCE(cancellation_verified_at,NOW()),message='Заказ отменён до создания документа СБИС',lease_token='',lease_until=NULL,updated_at=NOW() WHERE id=$1`, id, stateRaw); err != nil {
				return true, err
			}
			return true, tx.Commit(ctx)
		}
		switch progress.Stage {
		case "inspect-cancel", "discover-cancel", "verify-cancel", "reconcile-cancel":
		case "cancel":
			if status == "processing" || progress.Unknown {
				progress.Stage = "reconcile-cancel"
				progress.Unknown = true
			}
		default:
			progress.Stage = "inspect-cancel"
		}
	}
	if progress.IdentityConflict {
		return true, markInvalidClaim(ctx, tx, id, "Документ СБИС уже принадлежит другой реализации; требуется ручная сверка")
	}
	if progress.Stage == "" {
		return true, markInvalidClaim(ctx, tx, id, "Сохранённый этап реализации отсутствует")
	}
	if desired != "cancelled" && progress.Unknown && progress.DocumentID == "" {
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
	if writeStage(progress.Stage) && desired != "cancelled" {
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
	if progress.Stage == "post" && request.Event.Trigger == "created" {
		available, stockErr := createdStockReady(ctx, tx, request)
		if stockErr != nil {
			return true, stockErr
		}
		if !available {
			progress.Stage = "verify-fill"
			progress.StockBlocked = true
			raw, _ := json.Marshal(progress)
			if _, err = tx.Exec(ctx, `UPDATE order_sale_publications SET progress=$2 WHERE id=$1`, id, raw); err != nil {
				return true, err
			}
			return true, markInvalidClaim(ctx, tx, id, "Недостаточно свежего доступного остатка с учётом более ранних заказов; проведение не выполнялось")
		}
		progress.StockBlocked = false
	}
	if progress.Stage == "create" {
		createAttempted = true
		progress.CreateAttempted = true
	}
	if progress.Stage == "post" {
		postAttempted = true
		progress.PostAttempted = true
	}
	token, err := newOperationKey()
	if err != nil {
		return true, err
	}
	stateRaw, err = json.Marshal(progress)
	if err != nil {
		return true, err
	}
	if _, err = tx.Exec(ctx, `UPDATE order_sale_publications SET status='processing',progress=$2,lease_token=$3,lease_until=NOW()+INTERVAL '3 minutes',attempts=attempts+1,create_attempted=$4,post_attempted=$5,updated_at=NOW() WHERE id=$1`, id, stateRaw, token, createAttempted, postAttempted); err != nil {
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
	result.Progress.DesiredState = desired
	result.Progress.CreateAttempted = createAttempted
	result.Progress.PostAttempted = postAttempted
	if !retainsOwnedRowIDs(progress.OwnedRowIDs, result.Progress.OwnedRowIDs) {
		result = Result{Progress: progress, State: "needs_attention", Message: "Подтверждённые ID строк были утрачены; требуется сверка"}
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
	if result.State != "queued" && result.State != "posted" && result.State != "cancelled" && result.State != "needs_attention" && result.State != "failed" {
		result = Result{Progress: progress, State: "needs_attention", Message: "Неизвестный результат обмена; повторная запись не выполнялась"}
	}
	if result.State == "posted" && !validStockProof(request, result) {
		result.State = "needs_attention"
		result.Message = "Нет подтверждения проведения и складского движения; списание не подтверждено"
		result.StockVerified = false
		result.StockProof = nil
	}
	if result.State == "cancelled" && !validCancellationProof(request, result) {
		result.State = "needs_attention"
		result.Message = "Отмена проведения не подтверждена собственным документом и строками"
		result.CancellationVerified = false
		result.CancellationProof = nil
	}
	if result.State != "cancelled" {
		result.CancellationVerified = false
		result.CancellationProof = nil
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
	err = persistPublicationResult(ctx, pool, id, intentID, token, result, delay)
	return true, err
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
