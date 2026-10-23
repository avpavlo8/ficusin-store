package payment

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"time"
)

type settlementProvider interface {
	PrepareSettlementReceipt(string, string, integration.PaymentRequest, []integration.PaymentItem) (integration.SettlementReceiptRequest, error)
	CreateSettlementReceipt(context.Context, integration.SettlementReceiptRequest) (integration.FiscalReceipt, error)
	FetchReceipt(context.Context, string) (integration.FiscalReceipt, error)
}

// Explicit owner action only. No worker calls a POST on the receipt endpoint.
func (s *Service) SettlePaymentReceipt(ctx context.Context, orderID, paymentID, actorID int64) error {
	provider, ok := s.provider.(settlementProvider)
	if !ok || actorID <= 0 {
		return errors.New("формирование чека ЮKassa недоступно")
	}
	guard, err := s.lockOrderPreparation(ctx, orderID)
	if err != nil {
		return err
	}
	defer func() { _ = guard.Rollback(ctx) }()
	var providerID, orderStatus, receiptStatus string
	var raw []byte
	var net int64
	var pending int
	if err = s.pool.QueryRow(ctx, `SELECT p.provider_payment_id,p.request_payload,COALESCE(so.status,o.status),p.receipt_status,
 ROUND((p.amount-COALESCE((SELECT SUM(r.amount) FROM payment_refunds r WHERE r.payment_id=p.id AND r.status='succeeded'),0))*100)::BIGINT,
 (SELECT COUNT(*) FROM payment_refunds r WHERE r.payment_id=p.id AND r.status='pending')
 FROM payments p JOIN orders o ON o.id=p.order_id LEFT JOIN shipment_offers so ON so.id=p.shipment_offer_id WHERE p.id=$1 AND p.order_id=$2 AND p.status='paid'`, paymentID, orderID).Scan(&providerID, &raw, &orderStatus, &receiptStatus, &net, &pending); err != nil {
		return err
	}
	var exists bool
	if err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM payment_settlement_receipts WHERE payment_id=$1)`, paymentID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		if orderStatus != "completed" || receiptStatus != "succeeded" || pending > 0 || net <= 0 {
			return errors.New("чек зачёта доступен после выдачи товара, регистрации чека предоплаты и завершения возвратов")
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
		if _, err = s.pool.Exec(ctx, `INSERT INTO payment_settlement_receipts(payment_id,idempotence_key,request_payload,actor_id)VALUES($1,$2,$3,$4)`, paymentID, key, encoded, actorID); err != nil {
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
	if err := s.pool.QueryRow(ctx, `SELECT r.provider_receipt_id,r.status,r.request_payload,r.created_at,r.idempotence_key,p.provider_payment_id FROM payment_settlement_receipts r JOIN payments p ON p.id=r.payment_id WHERE r.payment_id=$1`, paymentID).Scan(&id, &status, &raw, &created, &key, &providerID); err != nil {
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
	rows, err := s.pool.Query(ctx, `SELECT p.order_id,r.payment_id FROM payment_settlement_receipts r JOIN payments p ON p.id=r.payment_id WHERE r.status='pending' AND r.provider_receipt_id<>'' AND r.updated_at<CURRENT_TIMESTAMP-INTERVAL '5 minutes' ORDER BY r.updated_at LIMIT 50`)
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
		err = s.syncSettlementReceipt(ctx, task.payment, false)
		_ = guard.Rollback(ctx)
		if err != nil {
			s.logger.Error("receipt reconciliation failed", "payment_id", task.payment, "error", err)
		}
	}
	return nil
}
