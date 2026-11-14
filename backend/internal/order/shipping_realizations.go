package order

import (
	"context"
	"errors"
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
	// Resolve an offer's owner without taking its row lock. Cancellation already
	// holds the parent before its order-sale lock and any offer updates; taking
	// the offer first would invert that order and deadlock the two transactions.
	if offerID > 0 {
		var parentID int64
		if err = tx.QueryRow(ctx, `SELECT order_id FROM shipment_offers WHERE id=$1`, offerID).Scan(&parentID); err != nil {
			return pgconn.CommandTag{}, err
		}
		if orderID > 0 && orderID != parentID {
			return pgconn.CommandTag{}, errors.New("отправка относится к другому заказу")
		}
		orderID = parentID
	}
	if orderID <= 0 {
		return pgconn.CommandTag{}, errors.New("не указан заказ отправления")
	}
	var lockedParent int64
	if err = tx.QueryRow(ctx, `SELECT id FROM orders WHERE id=$1 FOR UPDATE`, orderID).Scan(&lockedParent); err != nil {
		return pgconn.CommandTag{}, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('order-sale:'||$1::BIGINT::TEXT,0))`, orderID); err != nil {
		return pgconn.CommandTag{}, err
	}
	if offerID > 0 {
		var currentParent int64
		if err = tx.QueryRow(ctx, `SELECT order_id FROM shipment_offers WHERE id=$1 FOR UPDATE`, offerID).Scan(&currentParent); err != nil {
			return pgconn.CommandTag{}, err
		}
		if currentParent != lockedParent {
			return pgconn.CommandTag{}, errors.New("связь отправки с заказом изменилась")
		}
	}
	command, err := tx.Exec(ctx, query, args...)
	if err != nil {
		return command, err
	}
	if changed && command.RowsAffected() > 0 {
		if _, err = ordersale.Enqueue(ctx, tx, orderID, offerID, "automatic"); err != nil {
			return command, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return command, err
	}
	return command, nil
}
