package admin

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OrderNotifier tells a customer their order moved on. It is optional: the
// panel works the same without notifications configured.
type OrderNotifier interface {
	NotifyOrderStatus(ctx context.Context, customerID int64, orderNumber, status string) error
}

type PostgresRepository struct {
	pool     *pgxpool.Pool
	notifier OrderNotifier
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// WithNotifier returns the repository wired to send status notifications.
func (repository *PostgresRepository) WithNotifier(notifier OrderNotifier) *PostgresRepository {
	repository.notifier = notifier
	return repository
}

func (repository *PostgresRepository) Dashboard(ctx context.Context) (Dashboard, error) {
	var dashboard Dashboard
	if err := repository.pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM products)::INTEGER,
			(SELECT COUNT(*) FROM product_variants)::INTEGER,
			(SELECT COUNT(*) FROM orders)::INTEGER,
			(SELECT COUNT(*) FROM customers)::INTEGER,
			(SELECT COUNT(*) FROM customers WHERE wholesale_status = 'pending')::INTEGER
	`).Scan(
		&dashboard.Products,
		&dashboard.Variants,
		&dashboard.Orders,
		&dashboard.Customers,
		&dashboard.WholesalePending,
	); err != nil {
		return Dashboard{}, fmt.Errorf("query dashboard totals: %w", err)
	}

	var syncRun SyncRun
	err := repository.pool.QueryRow(ctx, `
		SELECT status, started_at, items_updated, errors_count
		FROM sync_runs
		WHERE source = 'saby'
		ORDER BY started_at DESC
		LIMIT 1
	`).Scan(&syncRun.Status, &syncRun.StartedAt, &syncRun.ItemsUpdated, &syncRun.ErrorsCount)
	if err == nil {
		dashboard.LastSync = &syncRun
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return Dashboard{}, fmt.Errorf("query latest sync: %w", err)
	}

	rows, err := repository.pool.Query(ctx, `
		SELECT order_number, customer_name, total::DOUBLE PRECISION, status, created_at
		FROM orders
		ORDER BY created_at DESC
		LIMIT 6
	`)
	if err != nil {
		return Dashboard{}, fmt.Errorf("query recent orders: %w", err)
	}
	defer rows.Close()
	dashboard.RecentOrders = make([]RecentOrder, 0)
	for rows.Next() {
		var item RecentOrder
		if err := rows.Scan(
			&item.OrderNumber, &item.CustomerName, &item.Total, &item.Status, &item.CreatedAt,
		); err != nil {
			return Dashboard{}, fmt.Errorf("scan recent order: %w", err)
		}
		dashboard.RecentOrders = append(dashboard.RecentOrders, item)
	}
	if err := rows.Err(); err != nil {
		return Dashboard{}, fmt.Errorf("read recent orders: %w", err)
	}
	return dashboard, nil
}
