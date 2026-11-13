package order

import (
	"context"
	"github.com/avpavlo8/ficusin-store/backend/internal/ordersale"
	"github.com/jackc/pgx/v5/pgconn"
)

// The verified carrier milestone and its internal realization intent commit
// together. This does not call Saby or require a configured external adapter.
func (worker *ShippingWorker) storeShipmentStatus(ctx context.Context, orderID, offerID int64, changed bool, query string, args ...any) (pgconn.CommandTag, error) {
	tx, err := worker.pool.Begin(ctx)
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	defer tx.Rollback(ctx)
	command, err := tx.Exec(ctx, query, args...)
	if err != nil {
		return command, err
	}
	if changed && command.RowsAffected() > 0 {
		if offerID > 0 {
			if err = tx.QueryRow(ctx, `SELECT order_id FROM shipment_offers WHERE id=$1`, offerID).Scan(&orderID); err != nil {
				return command, err
			}
		}
		if _, err = ordersale.Enqueue(ctx, tx, orderID, offerID, "automatic"); err != nil {
			return command, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return command, err
	}
	return command, nil
}
