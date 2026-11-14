package payment

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
)

type settlementProvider interface {
	PrepareSettlementReceipt(string, string, integration.PaymentRequest, []integration.PaymentItem) (integration.SettlementReceiptRequest, error)
	CreateSettlementReceipt(context.Context, integration.SettlementReceiptRequest) (integration.FiscalReceipt, error)
	FetchReceipt(context.Context, string) (integration.FiscalReceipt, error)
}

// The owner endpoint remains available for reviewed historical purchases. New
// completion events use the same durable preparation path from the worker.
func (s *Service) SettlePaymentReceipt(ctx context.Context, orderID, paymentID, actorID int64) error {
	if actorID <= 0 {
		return errors.New("формирование чека ЮKassa недоступно")
	}
	return s.settlePaymentReceipt(ctx, orderID, paymentID, &actorID, false)
}

func (s *Service) settlePaymentReceipt(ctx context.Context, orderID, paymentID int64, actorID *int64, automatic bool) error {
	provider, ok := s.provider.(settlementProvider)
	if !ok {
		return errors.New("формирование чека ЮKassa недоступно")
	}
	guard, err := s.lockOrderPreparation(ctx, orderID)
	if err != nil {
		return err
	}
	defer func() { _ = guard.Rollback(ctx) }()
	var providerID, orderStatus, receiptStatus, mode string
	var raw []byte
	var net, amount int64
	var pending int
	var test *bool
	var requested *time.Time
	if err = s.pool.QueryRow(ctx, `SELECT p.provider_payment_id,p.request_payload,COALESCE(so.status,o.status),p.receipt_status,
 ROUND((p.amount-COALESCE((SELECT SUM(r.amount) FROM payment_refunds r WHERE r.payment_id=p.id AND r.status='succeeded'),0))*100)::BIGINT,
 (SELECT COUNT(*) FROM payment_refunds r WHERE r.payment_id=p.id AND (r.status='pending' OR r.needs_review)),
 ROUND(p.amount*100)::BIGINT,p.provider_test,p.settlement_requested_at,payment_fiscal_mode(p.request_payload)
 FROM payments p JOIN orders o ON o.id=p.order_id LEFT JOIN shipment_offers so ON so.id=p.shipment_offer_id WHERE p.id=$1 AND p.order_id=$2 AND p.status='paid'`, paymentID, orderID).Scan(&providerID, &raw, &orderStatus, &receiptStatus, &net, &pending, &amount, &test, &requested, &mode); err != nil {
		return err
	}
	if test != nil && *test {
		return errors.New("для тестового платежа рабочий чек не создаётся")
	}
	var exists bool
	if err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM payment_settlement_receipts WHERE payment_id=$1)`, paymentID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		if automatic && requested == nil {
			return nil
		}
		if mode != "full_prepayment" {
			return errors.New("чек зачёта создаётся только по подтверждённому составу предоплаты")
		}
		if orderStatus != "completed" || receiptStatus != "succeeded" || pending > 0 || net <= 0 {
			return errors.New("чек зачёта доступен после выдачи товара, регистрации чека предоплаты и завершения возвратов")
		}
		if automatic {
			if _, err := s.pool.Exec(ctx, `UPDATE payments SET settlement_checked_at=CURRENT_TIMESTAMP WHERE id=$1`, paymentID); err != nil {
				return err
			}
			result, err := s.refreshPaymentReceipt(ctx, paymentID, providerID, amount)
			if err != nil {
				return err
			}
			if result.Test == nil || *result.Test || !result.Paid || result.ReceiptStatus != "succeeded" {
				return errors.New("реальная оплата и регистрация чека предоплаты не подтверждены ЮKassa")
			}
		}
		var original integration.PaymentRequest
		if len(raw) == 0 || json.Unmarshal(raw, &original) != nil {
			return errors.New("нет сохранённого состава платежа; нужна сверка в ЮKassa")
		}
		options, err := s.refundOptions(ctx, orderID)
		if err != nil {
			return err
		}
		items := []integration.PaymentItem{}
		var total int64
		for _, option := range options {
			if option.PaymentID == paymentID {
				items = append(items, option.original)
				items[len(items)-1].Quantity = option.Quantity
				total += cents(option.Price) * int64(option.Quantity)
			}
		}
		if total != net {
			return errors.New("оставшиеся позиции не совпадают с оплатой после возвратов; нужна сверка в ЮKassa")
		}
		key, err := idempotenceKey()
		if err != nil {
			return err
		}
		request, err := provider.PrepareSettlementReceipt(providerID, key, original, items)
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(request)
		if err != nil {
			return err
		}
		source := "owner"
		if automatic {
			source = "automatic"
		}
		if _, err = s.pool.Exec(ctx, `INSERT INTO payment_settlement_receipts(payment_id,idempotence_key,request_payload,actor_id,source)VALUES($1,$2,$3,$4,$5)`, paymentID, key, encoded, actorID, source); err != nil {
			return err
		}
	}
	return s.syncSettlementReceipt(ctx, paymentID, true)
}
func (s *Service) syncSettlementReceipt(ctx context.Context, paymentID int64, allowPost bool) error {
	provider, ok := s.provider.(settlementProvider)
	if !ok {
		return nil
	}
	var id, status, key, providerID string
	var raw []byte
	var created time.Time
	var test *bool
	if err := s.pool.QueryRow(ctx, `SELECT r.provider_receipt_id,r.status,r.request_payload,r.created_at,r.idempotence_key,p.provider_payment_id,p.provider_test FROM payment_settlement_receipts r JOIN payments p ON p.id=r.payment_id WHERE r.payment_id=$1`, paymentID).Scan(&id, &status, &raw, &created, &key, &providerID, &test); err != nil {
		return err
	}
	if status == "succeeded" {
		return nil
	}
	if status == "canceled" || status == "manual_review" || status == "failed" {
		return errors.New("чек требует сверки в ЮKassa; новый запрос не отправлен")
	}
	var request integration.SettlementReceiptRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return err
	}
	if request.Key != key || request.PaymentID != providerID || request.Body["payment_id"] != providerID {
		return errors.New("сохранённый запрос чека не соответствует платежу")
	}

	var receipt integration.FiscalReceipt
	var err error
	if id != "" {
		receipt, err = provider.FetchReceipt(ctx, id)
	} else {
		if !allowPost {
			return nil
		}
		if test != nil && *test {
			return errors.New("для тестового платежа рабочий чек не создаётся")
		}
		if time.Since(created) >= automaticRecoveryWindow {
			_, err = s.pool.Exec(ctx, `UPDATE payment_settlement_receipts SET status='manual_review',last_error='Сверьте чек в ЮKassa; повторная отправка после срока идемпотентности запрещена',updated_at=CURRENT_TIMESTAMP WHERE payment_id=$1`, paymentID)
			if err != nil {
				return err
			}
			return errors.New("срок повтора чека истёк; сверьте результат в ЮKassa")
		}
		receipt, err = provider.CreateSettlementReceipt(ctx, request)
	}
	if err != nil {
		status = "pending"
		if id == "" && !errors.Is(err, integration.ErrPaymentOutcomeUnknown) {
			status = "failed"
		}
		_, saveErr := s.pool.Exec(ctx, `UPDATE payment_settlement_receipts SET status=$2,last_error=$3,updated_at=CURRENT_TIMESTAMP WHERE payment_id=$1`, paymentID, status, err.Error())
		if saveErr != nil {
			return saveErr
		}
		return err
	}
	if receipt.PaymentID != request.PaymentID || (id != "" && receipt.ID != id) || receipt.ID == "" || receipt.Type != "payment" || (receipt.Status != "pending" && receipt.Status != "succeeded" && receipt.Status != "canceled") {
		_, err = s.pool.Exec(ctx, `UPDATE payment_settlement_receipts SET status='manual_review',last_error='Ответ ЮKassa не соответствует чеку',updated_at=CURRENT_TIMESTAMP WHERE payment_id=$1`, paymentID)
		return err
	}
	_, err = s.pool.Exec(ctx, `UPDATE payment_settlement_receipts SET provider_receipt_id=$2,status=$3,last_error='',updated_at=CURRENT_TIMESTAMP WHERE payment_id=$1`, paymentID, receipt.ID, receipt.Status)
	return err
}
func (s *Service) reconcileSettlementReceipts(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `SELECT p.order_id,r.payment_id FROM payment_settlement_receipts r JOIN payments p ON p.id=r.payment_id
	 WHERE r.status='pending' AND p.provider_test IS DISTINCT FROM true
	 AND r.updated_at<CURRENT_TIMESTAMP-INTERVAL '1 minute'
	 ORDER BY r.updated_at LIMIT 50`)
	if err != nil {
		return err
	}
	type task struct{ order, payment int64 }
	tasks := []task{}
	for rows.Next() {
		var t task
		if err = rows.Scan(&t.order, &t.payment); err != nil {
			rows.Close()
			return err
		}
		tasks = append(tasks, t)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, task := range tasks {
		guard, err := s.lockOrderPreparation(ctx, task.order)
		if err != nil {
			return err
		}
		// Known IDs use GET; an unknown POST outcome reuses the saved request
		// and key inside the same 23-hour window as the owner retry path.
		err = s.syncSettlementReceipt(ctx, task.payment, true)
		_ = guard.Rollback(ctx)
		if err != nil {
			s.logger.Error("receipt reconciliation failed", "payment_id", task.payment, "error", err)
		}
	}
	return nil
}

func (s *Service) createCompletedSettlementReceipts(ctx context.Context) error {
	if _, ok := s.provider.(settlementProvider); !ok {
		return nil
	}
	rows, err := s.pool.Query(ctx, `SELECT p.order_id,p.id FROM payments p
	 JOIN orders o ON o.id=p.order_id LEFT JOIN shipment_offers so ON so.id=p.shipment_offer_id
	 LEFT JOIN payment_settlement_receipts r ON r.payment_id=p.id
	 WHERE p.settlement_requested_at IS NOT NULL AND p.status='paid' AND p.provider_payment_id<>''
	 AND p.provider_test IS DISTINCT FROM true AND p.receipt_status='succeeded'
	 AND (p.settlement_checked_at IS NULL OR p.settlement_checked_at<CURRENT_TIMESTAMP-INTERVAL '5 minutes')
	 AND COALESCE(so.status,o.status)='completed' AND r.payment_id IS NULL
	 AND payment_fiscal_mode(p.request_payload)='full_prepayment'
	 AND NOT EXISTS(SELECT 1 FROM payment_refunds f WHERE f.payment_id=p.id AND (f.status='pending' OR f.needs_review))
	 AND p.amount>COALESCE((SELECT SUM(f.amount) FROM payment_refunds f WHERE f.payment_id=p.id AND f.status='succeeded'),0)
	 ORDER BY p.settlement_checked_at NULLS FIRST,p.settlement_requested_at,p.id LIMIT 50`)
	if err != nil {
		return err
	}
	type task struct{ order, payment int64 }
	tasks := []task{}
	for rows.Next() {
		var t task
		if err = rows.Scan(&t.order, &t.payment); err != nil {
			rows.Close()
			return err
		}
		tasks = append(tasks, t)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, t := range tasks {
		if err = s.settlePaymentReceipt(ctx, t.order, t.payment, nil, true); err != nil {
			s.logger.Error("automatic settlement receipt failed", "payment_id", t.payment, "error", err)
		}
	}
	return nil
}
