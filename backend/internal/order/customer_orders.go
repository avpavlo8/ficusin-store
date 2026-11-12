package order

import (
	"context"
	"fmt"
)

// ListPageForCustomer counts the complete owned history before paging one view.
// Unknown and legacy nonterminal statuses stay in work, so active orders cannot
// disappear behind a page of more recent completed or cancelled orders.
func (repository *PostgresRepository) ListPageForCustomer(ctx context.Context, customerID int64, view string, limit, offset int) (CustomerOrderPage, error) {
	page := CustomerOrderPage{Orders: make([]Summary, 0)}
	if view != "work" && view != "completed" && view != "cancelled" {
		return page, fmt.Errorf("invalid customer order view")
	}
	if limit < 1 || limit > 50 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	if err := repository.pool.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE status NOT IN ('completed','cancelled')),
			COUNT(*) FILTER (WHERE status='completed'), COUNT(*) FILTER (WHERE status='cancelled')
		FROM orders WHERE customer_id=$1
	`, customerID).Scan(&page.Counts.Work, &page.Counts.Completed, &page.Counts.Cancelled); err != nil {
		return page, fmt.Errorf("count customer order views: %w", err)
	}
	rows, err := repository.pool.Query(ctx, `
		SELECT o.order_number,o.delivery_method,o.delivery_payee,o.total::DOUBLE PRECISION,
			o.status,o.created_at,COALESCE(SUM(oi.quantity),0)::INTEGER
		FROM orders o LEFT JOIN order_items oi ON oi.order_id=o.id
		WHERE o.customer_id=$1 AND (
			($2='work' AND o.status NOT IN ('completed','cancelled')) OR o.status=$2)
		GROUP BY o.id ORDER BY o.created_at DESC,o.id DESC LIMIT $3 OFFSET $4
	`, customerID, view, limit+1, offset)
	if err != nil {
		return page, fmt.Errorf("query customer order page: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item Summary
		if err := rows.Scan(&item.OrderNumber, &item.DeliveryMethod, &item.DeliveryPayee, &item.Total, &item.Status, &item.CreatedAt, &item.ItemsCount); err != nil {
			return page, fmt.Errorf("scan customer order page: %w", err)
		}
		page.Orders = append(page.Orders, item)
	}
	if err := rows.Err(); err != nil {
		return page, fmt.Errorf("read customer order page: %w", err)
	}
	page.HasMore = len(page.Orders) > limit
	if page.HasMore {
		page.Orders = page.Orders[:limit]
	}
	return page, nil
}
