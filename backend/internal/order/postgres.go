package order

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
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

// DetailForCustomer loads one order, scoped to its owner so a customer can
// never open somebody else's order by guessing an order number.
func (repository *PostgresRepository) DetailForCustomer(
	ctx context.Context,
	customerID int64,
	orderNumber string,
) (*Detail, error) {
	var detail Detail
	var orderID int64
	err := repository.pool.QueryRow(ctx, `
		SELECT
			o.id, o.order_number, o.delivery_method, o.address, o.comment,
			o.customer_name, o.phone, o.email, o.status, o.payment_method, o.payment_status,
			COALESCE(o.cdek_track_number, ''), o.has_preorder = 1,
			o.delivery_fee::DOUBLE PRECISION,
			o.delivery_fee_pending = 1, o.delivery_repack_requested = 1,
			o.subtotal::DOUBLE PRECISION,
			o.total::DOUBLE PRECISION,
			COALESCE((SELECT SUM(p.amount) FROM payments p WHERE p.order_id=o.id AND p.status='paid'),0)::DOUBLE PRECISION,
			COALESCE((SELECT SUM(r.amount) FROM payment_refunds r WHERE r.order_id=o.id AND r.status='succeeded'),0)::DOUBLE PRECISION,
			GREATEST(o.total
				- COALESCE((SELECT SUM(p.amount) FROM payments p WHERE p.order_id=o.id AND p.status='paid'),0)
				+ COALESCE((SELECT SUM(r.amount) FROM payment_refunds r WHERE r.order_id=o.id AND r.status='succeeded'),0),0)::DOUBLE PRECISION,
			(o.delivery_fee_pending=0 AND o.has_preorder=0 AND o.payment_method='online'
				AND o.status NOT IN ('cancelled','completed')),
			o.created_at
		FROM orders o
		WHERE o.customer_id = $1 AND o.order_number = $2
		LIMIT 1
	`, customerID, orderNumber).Scan(
		&orderID,
		&detail.OrderNumber,
		&detail.DeliveryMethod,
		&detail.Address,
		&detail.Comment,
		&detail.CustomerName,
		&detail.Phone,
		&detail.Email,
		&detail.Status,
		&detail.PaymentMethod,
		&detail.PaymentStatus,
		&detail.TrackNumber,
		&detail.HasPreorder,
		&detail.DeliveryFee,
		&detail.DeliveryFeePending,
		&detail.RepackRequested,
		&detail.Subtotal,
		&detail.Total,
		&detail.PaidAmount,
		&detail.RefundedAmount,
		&detail.AmountDue,
		&detail.PaymentReady,
		&detail.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query customer order: %w", err)
	}

	rows, err := repository.pool.Query(ctx, `
		SELECT product_name, unit_price::DOUBLE PRECISION, quantity
		FROM order_items
		WHERE order_id = $1
		ORDER BY id
	`, orderID)
	if err != nil {
		return nil, fmt.Errorf("query order items: %w", err)
	}
	defer rows.Close()

	detail.Items = make([]Item, 0)
	for rows.Next() {
		var item Item
		if err := rows.Scan(&item.ProductName, &item.UnitPrice, &item.Quantity); err != nil {
			return nil, fmt.Errorf("scan order item: %w", err)
		}
		detail.Items = append(detail.Items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read order items: %w", err)
	}
	return &detail, nil
}
