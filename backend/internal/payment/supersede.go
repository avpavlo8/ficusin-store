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
// We retire the attempt locally under the same payment lock used by capture.
// A later authorization on a two-stage attempt will be canceled by the worker.
// The provider id stays on the row so a late legacy one-stage charge remains
// visible and can be reconciled against the current order.
func (service *Service) SupersedePending(ctx context.Context, orderID int64) error {
	if service == nil || service.pool == nil {
		return nil
	}
	rows, err := service.pool.Query(ctx, `SELECT id FROM payments WHERE order_id=$1 AND status=$2 ORDER BY id`, orderID, StatusPending)
	if err != nil {
		return err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		tx, err := service.lockPaymentOperation(ctx, id)
		if err != nil {
			return err
		}
		result, err := tx.Exec(ctx, `UPDATE payments SET status=$2,updated_at=CURRENT_TIMESTAMP WHERE id=$1 AND status=$3`, id, StatusSuperseded, StatusPending)
		if err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("supersede pending payment: %w", err)
		}
		if result.RowsAffected() == 0 {
			var status string
			if err := tx.QueryRow(ctx, `SELECT status FROM payments WHERE id=$1`, id).Scan(&status); err != nil {
				_ = tx.Rollback(ctx)
				return err
			}
			if status == StatusPaid {
				_ = tx.Rollback(ctx)
				return ErrPaymentNeedsReview
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}
