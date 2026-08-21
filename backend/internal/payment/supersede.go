package payment

import (
	"context"
	"fmt"
)

const StatusSuperseded = "superseded"

// SupersedePending retires payment pages that were created for the previous
// version of a mutable order.
//
// YooKassa one-stage payments are created in provider status "pending" while
// the buyer is still on the confirmation page. YooKassa does not allow the
// /cancel operation for that status; /cancel is only available after a
// two-stage payment reaches waiting_for_capture. Treating provider cancel as
// a prerequisite therefore made every order edit roll back while an unpaid
// link existed.
//
// We retire the attempt locally instead. It will no longer be reused and the
// partial unique index immediately allows a new payment attempt for the new
// amount. The provider id stays on the row: if the buyer manages to finish an
// old page, the webhook can still find that exact attempt and reconcile the
// money against the current order.
func (service *Service) SupersedePending(ctx context.Context, orderID int64) error {
	if service == nil || service.pool == nil {
		return nil
	}
	if _, err := service.pool.Exec(ctx, `
		UPDATE payments
		SET status=$2, updated_at=CURRENT_TIMESTAMP
		WHERE order_id=$1 AND status=$3
	`, orderID, StatusSuperseded, StatusPending); err != nil {
		return fmt.Errorf("supersede pending payment: %w", err)
	}
	return nil
}
