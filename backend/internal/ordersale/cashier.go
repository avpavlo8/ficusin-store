package ordersale

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// The cashier owns the physical sale for payment on receipt. Never prepare a
// second warehouse sale from checkout, completion, or manual batch preparation.
func cashierOwnsSale(ctx context.Context, q queryer, orderID int64) (bool, error) {
	var cashier bool
	err := q.QueryRow(ctx, `SELECT COALESCE(payment_method='on_delivery',FALSE) FROM orders WHERE id=$1`, orderID).Scan(&cashier)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil // Preserve the caller's existing not-found semantics.
	}
	return cashier, err
}
