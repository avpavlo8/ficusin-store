package procurement

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// allocateRequests assigns only the quantity that a purchase line can cover.
// Existing active allocations are subtracted, so adding the same product to a
// later purchase never counts a request twice.
func allocateRequests(ctx context.Context, tx pgx.Tx, lineID int64, sabyID string, capacity int) error {
	rows, err := tx.Query(ctx, `
		SELECT r.id, r.quantity-COALESCE(a.quantity,0)
		FROM procurement_requests r
		LEFT JOIN LATERAL (SELECT SUM(active_quantity)::INTEGER quantity FROM procurement_request_allocations
			WHERE request_id=r.id AND status='active') a ON TRUE
		WHERE r.saby_id=$1 AND r.status IN ('open','included')
		ORDER BY CASE r.kind WHEN 'customer_order' THEN 0 ELSE 1 END,r.created_at,r.id
		FOR UPDATE OF r
	`, sabyID)
	if err != nil {
		return fmt.Errorf("load procurement requests for allocation: %w", err)
	}
	type remainder struct {
		id       int64
		quantity int
	}
	requests := make([]remainder, 0)
	for rows.Next() {
		var item remainder
		if err := rows.Scan(&item.id, &item.quantity); err != nil {
			rows.Close()
			return err
		}
		requests = append(requests, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, request := range requests {
		if capacity <= 0 {
			break
		}
		quantity := min(capacity, max(0, request.quantity))
		if quantity == 0 {
			continue
		}
		if _, err := tx.Exec(ctx, `INSERT INTO procurement_request_allocations(request_id,procurement_order_line_id,quantity,active_quantity)
			VALUES($1,$2,$3,$3) ON CONFLICT(request_id,procurement_order_line_id) DO NOTHING`, request.id, lineID, quantity); err != nil {
			return fmt.Errorf("allocate procurement request: %w", err)
		}
		capacity -= quantity
	}
	return refreshRequestStatuses(ctx, tx)
}

func refreshRequestStatuses(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `UPDATE procurement_requests r SET status=CASE
		WHEN COALESCE(a.fulfilled,0)>=r.quantity THEN 'fulfilled'
		WHEN COALESCE(a.active,0)>0 THEN 'included' ELSE 'open' END,updated_at=CURRENT_TIMESTAMP
	FROM (SELECT request_id,COALESCE(SUM(active_quantity) FILTER(WHERE status='active'),0) active,
		COALESCE(SUM(quantity) FILTER(WHERE status='fulfilled'),0) fulfilled
		FROM procurement_request_allocations GROUP BY request_id) a
	WHERE r.id=a.request_id AND r.status NOT IN('cancelled','fulfilled')`)
	if err != nil {
		return fmt.Errorf("refresh procurement request statuses: %w", err)
	}
	return nil
}

func releaseOrderAllocations(ctx context.Context, tx pgx.Tx, orderID int64, reason string) error {
	_, err := tx.Exec(ctx, `UPDATE procurement_request_allocations a SET active_quantity=0,status='released',release_reason=$2,updated_at=CURRENT_TIMESTAMP
		FROM procurement_order_lines l WHERE a.procurement_order_line_id=l.id AND l.procurement_order_id=$1 AND a.status='active'`, orderID, reason)
	if err != nil {
		return fmt.Errorf("release procurement request allocations: %w", err)
	}
	return refreshRequestStatuses(ctx, tx)
}

func fulfilOrderAllocations(ctx context.Context, tx pgx.Tx, orderID int64) error {
	_, err := tx.Exec(ctx, `UPDATE procurement_request_allocations a SET status='fulfilled',updated_at=CURRENT_TIMESTAMP
		FROM procurement_order_lines l WHERE a.procurement_order_line_id=l.id AND l.procurement_order_id=$1 AND a.status='active'`, orderID)
	if err != nil {
		return fmt.Errorf("fulfil procurement request allocations: %w", err)
	}
	return refreshRequestStatuses(ctx, tx)
}

// Releasing allocations above the invoiced quantity restores the missing part
// to recommendations. A line absent from an invoice has capacity zero.
func rebalanceInvoiceAllocations(ctx context.Context, tx pgx.Tx, orderID int64) error {
	rows, err := tx.Query(ctx, `SELECT id,COALESCE(invoiced_qty,0) FROM procurement_order_lines WHERE procurement_order_id=$1 ORDER BY id FOR UPDATE`, orderID)
	if err != nil {
		return err
	}
	type invoiceLine struct {
		id       int64
		capacity int
	}
	lines := make([]invoiceLine, 0)
	for rows.Next() {
		var line invoiceLine
		if err := rows.Scan(&line.id, &line.capacity); err != nil {
			rows.Close()
			return err
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, line := range lines {
		lineID, capacity := line.id, line.capacity
		allocs, err := tx.Query(ctx, `SELECT id,active_quantity FROM procurement_request_allocations WHERE procurement_order_line_id=$1 AND status='active' ORDER BY id FOR UPDATE`, lineID)
		if err != nil {
			return err
		}
		type allocation struct {
			id       int64
			quantity int
		}
		values := make([]allocation, 0)
		for allocs.Next() {
			var item allocation
			if err := allocs.Scan(&item.id, &item.quantity); err != nil {
				allocs.Close()
				return err
			}
			values = append(values, item)
		}
		if err := allocs.Err(); err != nil {
			allocs.Close()
			return err
		}
		allocs.Close()
		for _, item := range values {
			keep := min(capacity, item.quantity)
			capacity -= keep
			if keep < item.quantity {
				status := "active"
				if keep == 0 {
					status = "released"
				}
				if _, err := tx.Exec(ctx, `UPDATE procurement_request_allocations SET active_quantity=$2,status=$3,release_reason='invoice_shortage',updated_at=CURRENT_TIMESTAMP WHERE id=$1`, item.id, keep, status); err != nil {
					return err
				}
			}
		}
	}
	return refreshRequestStatuses(ctx, tx)
}
