package payment

import (
	"context"
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
	rows, err := s.pool.Query(ctx, `SELECT p.id,p.receipt_status,COALESCE(so.status,o.status),ROUND((p.amount-COALESCE((SELECT SUM(r.amount) FROM payment_refunds r WHERE r.payment_id=p.id AND r.status='succeeded'),0))*100)::bigint,COALESCE(sr.status,''),COALESCE(sr.provider_receipt_id,'') FROM payments p JOIN orders o ON o.id=p.order_id LEFT JOIN shipment_offers so ON so.id=p.shipment_offer_id LEFT JOIN payment_settlement_receipts sr ON sr.payment_id=p.id WHERE p.order_id=$1 AND p.status='paid' AND p.provider_payment_id<>'' ORDER BY p.id`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := []FiscalTask{}
	for rows.Next() {
		var id int64
		var status, orderStatus, settlementStatus, receiptID string
		var net int64
		if err = rows.Scan(&id, &status, &orderStatus, &net, &settlementStatus, &receiptID); err != nil {
			return nil, err
		}
		if status == "" {
			status = "unknown"
		}
		tasks = append(tasks, FiscalTask{id, "income", status, ""})
		if net > 0 && (orderStatus == "completed" || settlementStatus != "") {
			if settlementStatus == "" {
				settlementStatus = "ready"
			}
			tasks = append(tasks, FiscalTask{id, "settlement", settlementStatus, receiptID})
		}
	}
	return tasks, rows.Err()
}

// Only GET is used here, even for succeeded money transactions. Receipt polling
// must never call the payment state machine, capture or cancellation endpoints.
func (s *Service) reconcilePaymentReceipts(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `SELECT id,provider_payment_id,ROUND(amount*100)::BIGINT FROM payments WHERE status='paid' AND provider_payment_id<>'' AND receipt_status IN ('','pending','unknown','canceled') AND (receipt_checked_at IS NULL OR receipt_checked_at<CURRENT_TIMESTAMP-INTERVAL '5 minutes') ORDER BY receipt_checked_at NULLS FIRST,id LIMIT 50`)
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
		result, err := s.provider.FetchPayment(ctx, i.provider)
		if err != nil {
			s.logger.Error("fetch payment receipt failed", "payment_id", i.id, "error", err)
			continue
		}
		if result.ID != i.provider || result.Status != "succeeded" || cents(result.Amount) != i.minor {
			s.logger.Error("payment receipt response mismatch", "payment_id", i.id)
			continue
		}
		status := result.ReceiptStatus
		switch status {
		case "pending", "succeeded", "canceled":
		default:
			status = "unknown"
		}
		if _, err = s.pool.Exec(ctx, `UPDATE payments SET receipt_status=$2,receipt_checked_at=CURRENT_TIMESTAMP WHERE id=$1 AND status='paid'`, i.id, status); err != nil {
			return err
		}
	}
	return nil
}
