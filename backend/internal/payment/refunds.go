package payment

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/avpavlo8/ficusin-store/backend/internal/money"
	"strings"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/jackc/pgx/v5"
)

type refundActorKey struct{}
type refundRequestKey struct{}
type refundSelectionsKey struct{}

func WithRefundSelections(ctx context.Context, items []RefundSelection) context.Context {
	return context.WithValue(ctx, refundSelectionsKey{}, items)
}

func WithRefundRequest(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, refundRequestKey{}, id)
}

func WithRefundActor(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, refundActorKey{}, id)
}

type refundProvider interface {
	PrepareRefund(string, float64, string, integration.PaymentRequest) (integration.RefundRequest, error)
	CreateRefund(context.Context, integration.RefundRequest) (integration.Refund, error)
	FetchRefund(context.Context, string) (integration.Refund, error)
}

type RefundIssue struct {
	ID          int64   `json:"id"`
	Amount      float64 `json:"amount"`
	Status      string  `json:"status"`
	ProviderID  string  `json:"providerId"`
	ActorID     *int64  `json:"actorId,omitempty"`
	NeedsReview bool    `json:"needsReview"`

	LastError        string `json:"lastError"`
	ReceiptStatus    string `json:"receiptStatus"`
	ReceiptReference string `json:"receiptReference"`
}

func (service *Service) refundIssues(ctx context.Context, orderID int64) ([]RefundIssue, error) {
	rows, err := service.pool.Query(ctx, `SELECT id,amount::double precision,status,provider_refund_id,actor_id,needs_review,last_error,receipt_status FROM payment_refunds WHERE order_id=$1 ORDER BY id DESC`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []RefundIssue{}
	for rows.Next() {
		var item RefundIssue
		if err := rows.Scan(&item.ID, &item.Amount, &item.Status, &item.ProviderID, &item.ActorID, &item.NeedsReview, &item.LastError, &item.ReceiptStatus); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// The preparation lock serializes the decision and all durable attempts for
// this order. Each split is saved before any external request can be made.
func (service *Service) RefundAmount(ctx context.Context, orderID int64, amount float64, reason string) (Balance, error) {
	requested, inputErr := money.Exact(amount)
	if inputErr != nil || requested <= 0 {
		return Balance{}, errors.New("укажите положительную сумму возврата в целых копейках")
	}
	if !service.Configured() {
		return Balance{}, errors.New("возврат недоступен: оплата не настроена")
	}
	guard, err := service.lockOrderPreparation(ctx, orderID)
	if err != nil {
		return Balance{}, err
	}
	defer func() { _ = guard.Rollback(ctx) }()
	selections, _ := ctx.Value(refundSelectionsKey{}).([]RefundSelection)
	signature, err := selectionSignature(selections)
	if err != nil {
		return Balance{}, err
	}
	requestID, _ := ctx.Value(refundRequestKey{}).(string)
	if requestID != "" {
		var count int
		var total int64
		var savedReason, savedSignature string
		if err := service.pool.QueryRow(ctx, `SELECT COUNT(*),ROUND(COALESCE(SUM(amount),0)*100)::bigint,COALESCE(MIN(reason),''),COALESCE(MIN(selections::text),'[]') FROM payment_refunds WHERE order_id=$1 AND request_id=$2`, orderID, requestID).Scan(&count, &total, &savedReason, &savedSignature); err != nil {
			return Balance{}, err
		}
		if count > 0 {
			var saved []RefundSelection
			if json.Unmarshal([]byte(savedSignature), &saved) != nil {
				return Balance{}, errors.New("повреждён состав возврата")
			}
			canonicalSaved, _ := selectionSignature(saved)
			if total != requested || savedReason != strings.TrimSpace(reason) || canonicalSaved != signature {
				return Balance{}, errors.New("ключ возврата уже использован с другой суммой или причиной")
			}
			if _, err := service.resumePendingRefunds(ctx, orderID); err != nil {
				return Balance{}, err
			}
			return service.BalanceForOrder(ctx, orderID)
		}
	}
	if resumed, err := service.resumePendingRefunds(ctx, orderID); err != nil {
		return Balance{}, err
	} else if resumed {
		if requestID != "" {
			return Balance{}, errors.New("предыдущий возврат ещё не завершён; проверьте его результат перед новым возвратом")
		}
		return service.BalanceForOrder(ctx, orderID)
	}
	balance, err := service.BalanceForOrder(ctx, orderID)
	if err != nil {
		return balance, err
	}
	if requested <= 0 || requested > cents(balance.NetPaid) {
		return balance, errors.New("сумма возврата должна быть больше нуля и не превышать полученную оплату")
	}
	provider, ok := service.provider.(refundProvider)
	if !ok {
		return balance, errors.New("провайдер не поддерживает сверку возвратов")
	}
	payments, err := service.refundablePayments(ctx, orderID)
	if err != nil {
		return balance, err
	}
	type planned struct {
		paymentID int64
		request   integration.RefundRequest
		raw       []byte
	}
	plans := []planned{}
	selected, err := service.selectedRefunds(ctx, orderID, selections, requested)
	if err != nil {
		return balance, err
	}
	remaining := requested
	for _, p := range payments {
		if remaining <= 0 {
			break
		}
		part := min(remaining, p.available)
		if len(selections) > 0 {
			chosen, exists := selected[p.id]
			if !exists {
				continue
			}
			part = chosen.amount
			if part > p.available {
				return balance, errors.New("сумма позиций превышает остаток платежа")
			}
		}
		key, err := idempotenceKey()
		if err != nil {
			return balance, err
		}
		var raw []byte
		if err = service.pool.QueryRow(ctx, `SELECT request_payload FROM payments WHERE id=$1`, p.id).Scan(&raw); err != nil {
			return balance, err
		}
		var original integration.PaymentRequest
		if len(raw) > 0 {
			if err = json.Unmarshal(raw, &original); err != nil {
				return balance, err
			}
		}
		if len(selections) > 0 {
			original.Items = selected[p.id].items
		}
		var settlement string
		if err = service.pool.QueryRow(ctx, `SELECT COALESCE((SELECT status FROM payment_settlement_receipts WHERE payment_id=$1),'')`, p.id).Scan(&settlement); err != nil {
			return balance, err
		}
		if settlement == "pending" || settlement == "manual_review" {
			return balance, errors.New("сначала сверьте результат чека зачёта в ЮKassa; возврат не отправлен")
		}
		if settlement == "succeeded" {
			for i := range original.Items {
				original.Items[i].Mode = "full_payment"
			}
		}

		req, err := provider.PrepareRefund(p.providerID, rublesFromCents(part), key, original)
		if err != nil {
			return balance, err
		}
		payload, err := json.Marshal(req)
		if err != nil {
			return balance, err
		}
		plans = append(plans, planned{p.id, req, payload})
		remaining -= part
	}
	if remaining > 0 {
		return balance, errors.New("недостаточно возвратных платежей по заказу")
	}
	tx, err := service.pool.Begin(ctx)
	if err != nil {
		return balance, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var actor any
	if id, _ := ctx.Value(refundActorKey{}).(int64); id > 0 {
		actor = id
	}
	for _, p := range plans {
		if _, err = tx.Exec(ctx, `INSERT INTO payment_refunds(order_id,payment_id,idempotence_key,amount,reason,status,actor_id,request_payload,request_id,selections) VALUES($1,$2,$3,$4,$5,'pending',$6,$7::jsonb,$8,$9::jsonb)`, orderID, p.paymentID, p.request.IdempotenceKey, p.request.Amount, strings.TrimSpace(reason), actor, p.raw, requestID, signature); err != nil {
			return balance, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return balance, err
	}
	if _, err = service.resumePendingRefunds(ctx, orderID); err != nil {
		return balance, err
	}
	return service.Reconcile(ctx, orderID)
}

func (service *Service) resumePendingRefunds(ctx context.Context, orderID int64) (bool, error) {
	rows, err := service.pool.Query(ctx, `SELECT id FROM payment_refunds WHERE order_id=$1 AND status='pending' ORDER BY id`, orderID)
	if err != nil {
		return false, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return false, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return false, err
	}
	var firstErr error
	for _, id := range ids {
		if err := service.syncRefund(ctx, id); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if len(ids) > 0 {
		if _, err := service.Reconcile(ctx, orderID); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return len(ids) > 0, firstErr
}

func (service *Service) syncRefund(ctx context.Context, id int64) error {
	provider, ok := service.provider.(refundProvider)
	if !ok {
		return errors.New("провайдер не поддерживает сверку возвратов")
	}
	var raw []byte
	var providerID, paymentID, key, currentStatus string
	var amount float64
	var created time.Time
	var review bool
	err := service.pool.QueryRow(ctx, `SELECT r.request_payload,r.provider_refund_id,p.provider_payment_id,r.idempotence_key,r.amount::double precision,r.created_at,r.needs_review,r.status FROM payment_refunds r JOIN payments p ON p.id=r.payment_id WHERE r.id=$1 AND (r.status='pending' OR (r.status='succeeded' AND r.provider_refund_id<>'' AND r.receipt_status IN ('','pending','unknown','canceled','legacy_unverified')))`, id).Scan(&raw, &providerID, &paymentID, &key, &amount, &created, &review, &currentStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if review {
		return nil
	}
	var req integration.RefundRequest
	if providerID == "" && (len(raw) == 0 || time.Since(created) >= automaticRecoveryWindow) {
		_, err = service.pool.Exec(ctx, `UPDATE payment_refunds SET needs_review=true,last_error='Неизвестный результат возврата: сверьте в ЮKassa; повторная отправка запрещена',updated_at=CURRENT_TIMESTAMP WHERE id=$1`, id)
		return err
	}
	var result integration.Refund
	if providerID != "" {
		result, err = provider.FetchRefund(ctx, providerID)
	} else {
		if err = json.Unmarshal(raw, &req); err != nil {
			return err
		}
		if req.PaymentID != paymentID || req.IdempotenceKey != key || cents(req.Amount) != cents(amount) {
			return errors.New("сохранённый запрос возврата не совпадает с проводкой")
		}
		result, err = provider.CreateRefund(ctx, req)
	}
	if err != nil {
		// Only an ambiguous POST can be retried with the same key. A known ID is
		// always fetched, even after 24 hours. GET failures never cancel a refund.
		status := currentStatus
		if providerID == "" && !errors.Is(err, integration.ErrPaymentOutcomeUnknown) {
			status = "failed"
		}
		_, saveErr := service.pool.Exec(ctx, `UPDATE payment_refunds SET status=$2,last_error=$3,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, id, status, err.Error())
		if saveErr != nil {
			return saveErr
		}
		return err
	}
	if (currentStatus == "succeeded" && result.Status != "succeeded") || result.ID == "" || (providerID != "" && result.ID != providerID) || result.PaymentID != paymentID || cents(result.Amount) != cents(amount) || (result.Status != "pending" && result.Status != "succeeded" && result.Status != "canceled") {
		_, err = service.pool.Exec(ctx, `UPDATE payment_refunds SET needs_review=true,last_error='Ответ ЮKassa не соответствует возврату; нужна ручная сверка',updated_at=CURRENT_TIMESTAMP WHERE id=$1`, id)
		return err
	}
	// The exact persisted request decides whether our till still owes a receipt.
	if len(raw) > 0 {
		if err = json.Unmarshal(raw, &req); err != nil {
			return err
		}
	}
	_, err = service.pool.Exec(ctx, `UPDATE payment_refunds SET provider_refund_id=$2,status=$3,receipt_status=$4,receipt_checked_at=CURRENT_TIMESTAMP,last_error='',updated_at=CURRENT_TIMESTAMP WHERE id=$1 AND status IN ('pending','succeeded')`, id, result.ID, result.Status, refundReceiptStatus(req, result))
	return err
}

func (service *Service) reconcileRefunds(ctx context.Context) error {
	rows, err := service.pool.Query(ctx, `SELECT DISTINCT order_id FROM payment_refunds WHERE (status='pending' AND NOT needs_review) OR (status='succeeded' AND provider_refund_id<>'' AND receipt_status IN ('','pending','unknown','canceled','legacy_unverified') AND (receipt_checked_at IS NULL OR receipt_checked_at<CURRENT_TIMESTAMP-INTERVAL '5 minutes')) ORDER BY order_id LIMIT 50`)
	if err != nil {
		return err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		guard, err := service.lockOrderPreparation(ctx, id)
		if err != nil {
			return err
		}
		_, err = service.resumePendingRefunds(ctx, id)
		if err == nil {
			err = service.syncRefundReceipts(ctx, id)
		}
		_ = guard.Rollback(ctx)
		if err != nil {
			service.logger.Error("reconcile refund failed", "order_id", id, "error", err)
		}
	}
	return nil
}

// ResolveRefund links a verified provider refund after an ambiguous response.
// This is a read of YooKassa, never another POST that could move money twice.
func (service *Service) ResolveRefund(ctx context.Context, orderID, refundID int64, providerID string, actorID int64) error {
	provider, ok := service.provider.(refundProvider)
	if !ok {
		return errors.New("провайдер не поддерживает сверку возвратов")
	}
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return errors.New("укажите ID возврата из ЮKassa")
	}
	guard, err := service.lockOrderPreparation(ctx, orderID)
	if err != nil {
		return err
	}
	defer func() { _ = guard.Rollback(ctx) }()
	var paymentID, existing string
	var amount float64
	err = service.pool.QueryRow(ctx, `SELECT p.provider_payment_id,r.provider_refund_id,r.amount::double precision FROM payment_refunds r JOIN payments p ON p.id=r.payment_id WHERE r.id=$1 AND r.order_id=$2 AND r.status IN ('pending','succeeded')`, refundID, orderID).Scan(&paymentID, &existing, &amount)
	if err != nil {
		return err
	}
	if existing != "" && existing != providerID {
		return errors.New("у возврата уже есть другой ID ЮKassa")
	}
	result, err := provider.FetchRefund(ctx, providerID)
	if err != nil {
		return err
	}
	if result.ID != providerID || result.PaymentID != paymentID || cents(result.Amount) != cents(amount) || (result.Status != "pending" && result.Status != "succeeded" && result.Status != "canceled") {
		return errors.New("возврат ЮKassa не соответствует платежу или сумме заказа")
	}
	_, err = service.pool.Exec(ctx, `UPDATE payment_refunds SET provider_refund_id=$3,needs_review=false,resolved_by=$4,last_error='',updated_at=CURRENT_TIMESTAMP WHERE id=$1 AND order_id=$2 AND status IN ('pending','succeeded')`, refundID, orderID, providerID, actorID)
	if err != nil {
		return err
	}
	if err = service.syncRefund(ctx, refundID); err != nil {
		return err
	}
	_, err = service.Reconcile(ctx, orderID)
	return err
}
