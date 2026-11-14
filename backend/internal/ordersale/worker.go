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
	control, err := readDispatchControl(ctx, tx, false)
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
	err = tx.QueryRow(ctx, `SELECT p.id,p.intent_id,p.status,p.request,p.request_hash,p.progress,p.desired_state,p.create_attempted,p.post_attempted,p.cancellation_blocked FROM order_sale_publications p JOIN order_sale_intents i ON i.id=p.intent_id JOIN orders o ON o.id=i.order_id WHERE p.execution_mode=$1 AND ((p.status='queued' AND p.next_attempt_at<=NOW()) OR (p.status='processing' AND p.lease_until<NOW())) ORDER BY p.next_attempt_at,p.id FOR UPDATE OF o SKIP LOCKED LIMIT 1`, mode).Scan(&id, &intentID, &status, &raw, &hash, &stateRaw, &desired, &createAttempted, &postAttempted, &cancellationBlocked)
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
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('order-sale:'||$1::BIGINT::TEXT,0))`, request.Event.OrderID); err != nil {
		return true, err
	}
	if _, err = tx.Exec(ctx, `SELECT id FROM order_sale_intents WHERE id=$1 FOR UPDATE`, intentID); err != nil {
		return true, err
	}
	err = tx.QueryRow(ctx, `SELECT status,request,request_hash,progress,desired_state,create_attempted,post_attempted,cancellation_blocked FROM order_sale_publications WHERE id=$1 AND execution_mode=$2 AND ((status='queued' AND next_attempt_at<=NOW())OR(status='processing'AND lease_until<NOW())) FOR UPDATE`, id, mode).Scan(&status, &raw, &hash, &stateRaw, &desired, &createAttempted, &postAttempted, &cancellationBlocked)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	if json.Unmarshal(raw, &request) != nil {
		return true, markInvalidClaim(ctx, tx, id, "Сохранённый запрос реализации повреждён")
	}
	control, err = readDispatchControl(ctx, tx, true)
	if err != nil {
		return true, err
	}
	if !control.Enabled || control.Mode != mode {
		return false, nil
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
	revision, revisionErr := revisionForClaim(ctx, tx, request, &progress, status)
	if revisionErr != nil {
		if errors.Is(revisionErr, ErrNotReady) || errors.Is(revisionErr, ErrConflict) {
			return true, markInvalidClaim(ctx, tx, id, "Редакция заказа не готова: проверьте связь товаров СБИС и сохранённые планы")
		}
		return true, revisionErr
	}
	if desired == "cancelled" && revision == nil {
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
	if revision != nil && writeStage(progress.Stage) && desired != "cancelled" {
		ready, e := revisionPlanMappingsReady(ctx, tx, revision.Target)
		if e != nil {
			return true, e
		}
		if !ready {
			return true, markInvalidClaim(ctx, tx, id, "Связь товара активной редакции со СБИС изменилась; запись остановлена для сверки")
		}
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
		expected := request
		if revision != nil {
			expected = revision.Target
		}
		pending, pendErr := RevisionPending(ctx, tx, request.Event.OrderID)
		if pendErr != nil {
			return true, pendErr
		}
		// Creating the one empty owned document does not send the old goods.
		originCreateWithEdit := revision == nil && pending && progress.Stage == "create"
		activeRevisionSuperseded := revision != nil && current.Fingerprint != expected.Event.Fingerprint
		if (!current.CanPrepare && !activeRevisionSuperseded) || (!originCreateWithEdit && !activeRevisionSuperseded && current.Fingerprint != expected.Event.Fingerprint) || !activeWarehouse {
			return true, markInvalidClaim(ctx, tx, id, "Состав, связь товара или склад изменились; обмен остановлен для сверки")
		}
	}
	stockPlan := request
	if revision != nil {
		stockPlan = revision.Target
	}
	if desired != "cancelled" && (progress.Stage == "post" || progress.Stage == "revision-post" || progress.Stage == "revision-unpost") && stockPlan.Event.Trigger == "created" {
		available, stockErr := createdStockReady(ctx, tx, stockPlan)
		if stockErr != nil {
			return true, stockErr
		}
		if !available {
			if revision != nil {
				if progress.Stage == "revision-unpost" {
					progress.Stage = "revision-inspect"
				} else {
					progress.Stage = "revision-verify-delete"
				}
			} else {
				progress.Stage = "verify-fill"
			}
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
	if err = markRevisionAttempts(ctx, tx, revision, &progress); err != nil {
		return true, err
	}
	postAttempted = progress.PostAttempted
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
	var result Result
	var callErr error
	if revision != nil {
		rp, ok := provider.(RevisionProvider)
		if !ok {
			result = Result{Progress: progress, State: "needs_attention", Message: "Обновление существующей реализации не поддерживается подключённым провайдером"}
		} else {
			result, callErr = rp.StepRevision(callCtx, *revision, progress)
		}
	} else {
		result, callErr = provider.Step(callCtx, request, progress)
	}
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
	if revision != nil {
		validateRevisionResult(*revision, progress, &result)
	}
	if result.State == "posted" && revision == nil && !validStockProof(request, result) {
		result.State = "needs_attention"
		result.Message = "Нет подтверждения проведения и складского движения; списание не подтверждено"
		result.StockVerified = false
		result.StockProof = nil
	}
	if result.State == "cancelled" && revision == nil && !validCancellationProof(request, result) {
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
	err = persistPublicationResult(ctx, pool, id, intentID, token, result, delay, revision)
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
