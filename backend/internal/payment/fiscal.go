package payment

import (
	"context"
	"errors"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
)

type FiscalTask struct {
	PaymentID int64  `json:"paymentId"`
	Kind      string `json:"kind"`
	Status    string `json:"status"`
	Reference string `json:"reference"`
}

// Payment fiscal state comes exclusively from YooKassa; historical unknowns
// require reconciliation and cannot be marked registered by an operator.
func (s *Service) fiscalTasks(ctx context.Context, orderID int64) ([]FiscalTask, error) {
	rows, err := s.pool.Query(ctx, `SELECT p.id,p.receipt_status,COALESCE(so.status,o.status),ROUND((p.amount-COALESCE((SELECT SUM(r.amount) FROM payment_refunds r WHERE r.payment_id=p.id AND r.status='succeeded'),0))*100)::bigint,COALESCE(sr.status,''),COALESCE(sr.provider_receipt_id,''),payment_fiscal_mode(p.request_payload),p.settlement_requested_at FROM payments p JOIN orders o ON o.id=p.order_id LEFT JOIN shipment_offers so ON so.id=p.shipment_offer_id LEFT JOIN payment_settlement_receipts sr ON sr.payment_id=p.id WHERE p.order_id=$1 AND p.status='paid' AND p.provider_payment_id<>'' AND p.provider_test IS DISTINCT FROM true ORDER BY p.id`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := []FiscalTask{}
	for rows.Next() {
		var id int64
		var status, orderStatus, settlementStatus, receiptID, mode string
		var requested *time.Time
		var net int64
		if err = rows.Scan(&id, &status, &orderStatus, &net, &settlementStatus, &receiptID, &mode, &requested); err != nil {
			return nil, err
		}
		if status == "" {
			status = "unknown"
		}
		tasks = append(tasks, FiscalTask{id, "income", status, ""})
		if net > 0 && ((orderStatus == "completed" && mode != "full_payment") || settlementStatus != "") {
			if settlementStatus == "" {
				settlementStatus = "ready"
				if mode == "unknown" {
					settlementStatus = "manual_review"
				} else if requested != nil {
					settlementStatus = "queued"
				}
			}
			tasks = append(tasks, FiscalTask{id, "settlement", settlementStatus, receiptID})
		}
	}
	return tasks, rows.Err()
}

// Only GET is used here, even for succeeded money transactions. Receipt polling
// must never call the payment state machine, capture or cancellation endpoints.
func (s *Service) reconcilePaymentReceipts(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `SELECT id,provider_payment_id,ROUND(amount*100)::BIGINT FROM payments WHERE status='paid' AND provider_payment_id<>'' AND provider_test IS DISTINCT FROM true AND receipt_status IN ('','pending','unknown','canceled') AND (receipt_checked_at IS NULL OR receipt_checked_at<CURRENT_TIMESTAMP-INTERVAL '5 minutes') ORDER BY receipt_checked_at NULLS FIRST,id LIMIT 50`)
	if err != nil {
		return err
	}
	type item struct {
		id       int64
		provider string
		minor    int64
	}
	items := []item{}
	for rows.Next() {
		var i item
		if err = rows.Scan(&i.id, &i.provider, &i.minor); err != nil {
			rows.Close()
			return err
		}
		items = append(items, i)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, i := range items {
		_, err := s.refreshPaymentReceipt(ctx, i.id, i.provider, i.minor)
		if err != nil {
			s.logger.Error("fetch payment receipt failed", "payment_id", i.id, "error", err)
		}
	}
	return nil
}

// A provider lookup can update fiscal facts only after identity and amount
// match. A failed lookup is not proof of test mode or of a registered receipt.
func (s *Service) refreshPaymentReceipt(ctx context.Context, id int64, providerID string, minor int64) (integration.Payment, error) {
	result, err := s.provider.FetchPayment(ctx, providerID)
	if err == nil && (result.ID != providerID || result.Status != "succeeded" || cents(result.Amount) != minor) {
		err = errors.New("ответ ЮKassa не соответствует платежу")
	}
	if err != nil {
		// Record the attempted check too, so an unavailable historic ID cannot
		// monopolize every worker tick. Leave every fiscal and money fact intact.
		_, saveErr := s.pool.Exec(ctx, `UPDATE payments SET receipt_checked_at=CURRENT_TIMESTAMP WHERE id=$1`, id)
		return result, errors.Join(err, saveErr)
	}
	status := result.ReceiptStatus
	switch status {
	case "pending", "succeeded", "canceled":
	default:
		status = "unknown"
	}
	_, err = s.pool.Exec(ctx, `UPDATE payments SET receipt_status=$2,receipt_checked_at=CURRENT_TIMESTAMP,
	 provider_test=COALESCE($3,provider_test),
	 provider_test_source=CASE WHEN $3::boolean IS NOT NULL THEN 'yookassa' ELSE provider_test_source END
	 WHERE id=$1 AND status='paid'`, id, status, result.Test)
	return result, err
}
