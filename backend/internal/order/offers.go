package order

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// CancelOpenShipmentOffers closes unpaid offers and their unsent invitation
// letters in the same transaction that cancels the parent order. Paid offers
// remain visible for refund and fulfilment review.
func CancelOpenShipmentOffers(ctx context.Context, tx pgx.Tx, orderID int64) error {
	if _, err := tx.Exec(ctx, `
		UPDATE shipment_offers SET status='cancelled',checkout_locked_until=NULL,updated_at=CURRENT_TIMESTAMP
		WHERE order_id=$1 AND status IN ('draft','packaging_required','notifying','offered','payment_pending')
	`, orderID); err != nil {
		return fmt.Errorf("cancel open shipment offers: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE outbox SET cancelled_at=CURRENT_TIMESTAMP,cancel_reason='Заказ отменён',
			lock_token='',locked_until=NULL
		WHERE sent_at IS NULL AND cancelled_at IS NULL AND shipment_offer_id IN
			(SELECT id FROM shipment_offers WHERE order_id=$1 AND status='cancelled')
	`, orderID); err != nil {
		return fmt.Errorf("cancel shipment offer letters: %w", err)
	}
	return nil
}
