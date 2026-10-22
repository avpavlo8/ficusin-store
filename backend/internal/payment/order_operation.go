package payment

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// A waiting request must not hold a pool connection: the winner needs another
// connection for its payment queries. Share the key with packing confirmation.
func (service *Service) lockOrderPreparation(ctx context.Context, orderID int64) (pgx.Tx, error) {
	for {
		tx, err := service.pool.Begin(ctx)
		if err != nil {
			return nil, err
		}
		var acquired bool
		err = tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(7241,$1::integer)`, orderID).Scan(&acquired)
		if err == nil && acquired {
			return tx, nil
		}
		_ = tx.Rollback(ctx)
		if err != nil {
			return nil, err
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
