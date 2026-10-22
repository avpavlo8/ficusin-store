package payment

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/jackc/pgx/v5"
)

type refundActorKey struct{}
type refundRequestKey struct{}

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
	ID              int64   `json:"id"`
	Amount          float64 `json:"amount"`
	Status          string  `json:"status"`
	ProviderID      string  `json:"providerId"`
	ActorID         *int64  `json:"actorId,omitempty"`
	NeedsReview     bool    `json:"needsReview"`
	ReceiptRequired bool    `json:"receiptRequired"`
	LastError       string  `json:"lastError"`
}

func (service *Service) refundIssues(ctx context.Context, orderID int64) ([]RefundIssue, error) {
	rows, err := service.pool.Query(ctx, `SELECT id,amount::double precision,status,provider_refund_id,actor_id,needs_review,receipt_required,last_error FROM payment_refunds WHERE order_id=$1 ORDER BY id DESC`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []RefundIssue{}
	for rows.Next() {
		var item RefundIssue
		if err := rows.Scan(&item.ID, &item.Amount, &item.Status, &item.ProviderID, &item.ActorID, &item.NeedsReview, &item.ReceiptRequired, &item.LastError); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// The preparation lock serializes the decision and all durable attempts for
// this order. Each split is saved before any external request can be made.
func (service *Service) RefundAmount(ctx context.Context, orderID int64, amount float64, reason string) (Balance, error) {
	if !service.Configured() {
		return Balance{}, errors.New("возврат недоступен: оплата не настроена")
	}
	guard, err := service.lockOrderPreparation(ctx, orderID)
	if err != nil {
		return Balance{}, err
	}
	defer func() { _ = guard.Rollback(ctx) }()
	requestID, _ := ctx.Value(refundRequestKey{}).(string)
	if requestID != "" {
		var count int
		var total float64
		var savedReason string
		if err := service.pool.QueryRow(ctx, `SELECT COUNT(*),COALESCE(SUM(amount),0)::double precision,COALESCE(MIN(reason),'') FROM payment_refunds WHERE order_id=$1 AND request_id=$2`, orderID, requestID).Scan(&count, &total, &savedReason); err != nil {
			return Balance{}, err
		}
		if count > 0 {
			if cents(total) != cents(amount) || savedReason != strings.TrimSpace(reason) {
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
	requested := cents(amount)
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
	remaining := requested
	for _, p := range payments {
		if remaining <= 0 {
			break
		}
		part := min(remaining, p.available)
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
		if _, err = tx.Exec(ctx, `INSERT INTO payment_refunds(order_id,payment_id,idempotence_key,amount,reason,status,actor_id,request_payload,request_id) VALUES($1,$2,$3,$4,$5,'pending',$6,$7::jsonb,$8)`, orderID, p.paymentID, p.request.IdempotenceKey, p.request.Amount, strings.TrimSpace(reason), actor, p.raw, requestID); err != nil {
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
	var providerID, paymentID, key string
	var amount float64
	var created time.Time
	var review bool
	err := service.pool.QueryRow(ctx, `SELECT r.request_payload,r.provider_refund_id,p.provider_payment_id,r.idempotence_key,r.amount::double precision,r.created_at,r.needs_review FROM payment_refunds r JOIN payments p ON p.id=r.payment_id WHERE r.id=$1 AND r.status='pending'`, id).Scan(&raw, &providerID, &paymentID, &key, &amount, &created, &review)
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
		status := "pending"
		if providerID == "" && !errors.Is(err, integration.ErrPaymentOutcomeUnknown) {
			status = "failed"
		}
		_, saveErr := service.pool.Exec(ctx, `UPDATE payment_refunds SET status=$2,last_error=$3,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, id, status, err.Error())
		if saveErr != nil {
			return saveErr
		}
		return err
	}
	if result.ID == "" || (providerID != "" && result.ID != providerID) || result.PaymentID != paymentID || cents(result.Amount) != cents(amount) || (result.Status != "pending" && result.Status != "succeeded" && result.Status != "canceled") {
		_, err = service.pool.Exec(ctx, `UPDATE payment_refunds SET needs_review=true,last_error='Ответ ЮKassa не соответствует возврату; нужна ручная сверка',updated_at=CURRENT_TIMESTAMP WHERE id=$1`, id)
		return err
	}
	// The exact persisted request decides whether our till still owes a receipt.
	if len(raw) > 0 {
		if err = json.Unmarshal(raw, &req); err != nil {
			return err
		}
	}
	_, err = service.pool.Exec(ctx, `UPDATE payment_refunds SET provider_refund_id=$2,status=$3,receipt_required=($3='succeeded' AND $4),last_error='',updated_at=CURRENT_TIMESTAMP WHERE id=$1 AND status='pending'`, id, result.ID, result.Status, req.Receipt == nil)
	return err
}

func (service *Service) reconcileRefunds(ctx context.Context) error {
	rows, err := service.pool.Query(ctx, `SELECT DISTINCT order_id FROM payment_refunds WHERE status='pending' AND NOT needs_review ORDER BY order_id LIMIT 50`)
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
		_ = guard.Rollback(ctx)
		if err != nil {
			service.logger.Error("reconcile refund failed", "order_id", id, "error", err)
		}
	}
	return nil
}

func (service *Service) ConfirmRefundReceipt(ctx context.Context, orderID, refundID, actorID int64) error {
	tag, err := service.pool.Exec(ctx, `UPDATE payment_refunds SET receipt_required=false,updated_at=CURRENT_TIMESTAMP,receipt_confirmed_by=$3 WHERE id=$1 AND order_id=$2 AND status='succeeded' AND receipt_required`, refundID, orderID, actorID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errors.New("чек возврата не ожидается")
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
	err = service.pool.QueryRow(ctx, `SELECT p.provider_payment_id,r.provider_refund_id,r.amount::double precision FROM payment_refunds r JOIN payments p ON p.id=r.payment_id WHERE r.id=$1 AND r.order_id=$2 AND r.status='pending'`, refundID, orderID).Scan(&paymentID, &existing, &amount)
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
	_, err = service.pool.Exec(ctx, `UPDATE payment_refunds SET provider_refund_id=$3,needs_review=false,resolved_by=$4,last_error='',updated_at=CURRENT_TIMESTAMP WHERE id=$1 AND order_id=$2 AND status='pending'`, refundID, orderID, providerID, actorID)
	if err != nil {
		return err
	}
	if err = service.syncRefund(ctx, refundID); err != nil {
		return err
	}
	_, err = service.Reconcile(ctx, orderID)
	return err
}
