package order

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (repository *PostgresRepository) ListForCustomer(
	ctx context.Context,
	customerID int64,
	limit int,
) ([]Summary, error) {
	rows, err := repository.pool.Query(ctx, `
		SELECT
			o.order_number,
			o.delivery_method,
			o.total::DOUBLE PRECISION,
			o.status,
			o.created_at,
			COALESCE(SUM(oi.quantity), 0)::INTEGER
		FROM orders o
		LEFT JOIN order_items oi ON oi.order_id = o.id
		WHERE o.customer_id = $1
		GROUP BY o.id
		ORDER BY o.created_at DESC
		LIMIT $2
	`, customerID, limit)
	if err != nil {
		return nil, fmt.Errorf("query customer orders: %w", err)
	}
	defer rows.Close()

	orders := make([]Summary, 0)
	for rows.Next() {
		var summary Summary
		if err := rows.Scan(
			&summary.OrderNumber,
			&summary.DeliveryMethod,
			&summary.Total,
			&summary.Status,
			&summary.CreatedAt,
			&summary.ItemsCount,
		); err != nil {
			return nil, fmt.Errorf("scan customer order: %w", err)
		}
		orders = append(orders, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read customer orders: %w", err)
	}
	return orders, nil
}
