package payment

import (
	"context"
	"fmt"
)

// CancelLegacyDeliveryPayments closes pending payment links created before
// delivery became payable directly to the carrier. A failed cancellation
// blocks readiness so an old link cannot collect delivery after the rollout.
// Successful charges are deliberately untouched; refunds require review.
func (service *Service) CancelLegacyDeliveryPayments(ctx context.Context) error {
	rows, err := service.pool.Query(ctx, `
		SELECT p.id, COALESCE(p.provider_payment_id, '')
		FROM payments p
		JOIN orders o ON o.id=p.order_id
		LEFT JOIN shipment_offers so ON so.id=p.shipment_offer_id
		WHERE p.status='pending'
		  AND ROUND(p.amount*100)>ROUND(CASE WHEN so.id IS NULL THEN o.subtotal ELSE so.subtotal END*100)
		ORDER BY p.id
	`)
	if err != nil {
		return fmt.Errorf("scan old payment links: %w", err)
	}
	type oldPayment struct {
		id         int64
		providerID string
	}
	var pending []oldPayment
	for rows.Next() {
		var current oldPayment
		if err := rows.Scan(&current.id, &current.providerID); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, current)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}
	if !service.Configured() {
		return fmt.Errorf("%d old payment links need cancellation, but YooKassa is not configured", len(pending))
	}
	closable, ok := service.provider.(canceller)
	if !ok {
		return ErrCancelUnsupported
	}
	for _, current := range pending {
		if current.providerID == "" {
			return fmt.Errorf("payment %d has no provider ID: %w", current.id, ErrPaymentNeedsReview)
		}
		key, err := idempotenceKey()
		if err != nil {
			return err
		}
		if err := closable.CancelPayment(ctx, current.providerID, key); err != nil {
			return fmt.Errorf("old payment %d was not cancelled: %w", current.id, err)
		}
		if _, err := service.pool.Exec(ctx, `UPDATE payments SET status=$2,updated_at=CURRENT_TIMESTAMP WHERE id=$1 AND status='pending'`, current.id, StatusCancelled); err != nil {
			return fmt.Errorf("record cancellation of payment %d: %w", current.id, err)
		}
	}
	return nil
}
