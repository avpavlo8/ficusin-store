package order

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/payment"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestConcurrentCheckoutDoesNotOversellOnLiveDatabase puts real concurrent
// transactions through the checkout service. Excess demand may become a
// preorder, but the database reservation must never exceed physical stock.
func TestConcurrentCheckoutDoesNotOversellOnLiveDatabase(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	emailPattern := "ci-load-" + suffix + "-%"

	var productID, variantID, warehouseID int64
	var sku string
	if err := pool.QueryRow(ctx, `
		INSERT INTO products(name, slug, status, category_id)
		SELECT 'CI concurrent checkout', $1, 'published', id
		FROM categories WHERE slug='accessories' RETURNING id
	`, "ci-load-"+suffix).Scan(&productID); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO product_variants(product_id, label, base_price_minor)
		VALUES ($1, 'CI load variant', 10000) RETURNING id, sku
	`, productID).Scan(&variantID, &sku); err != nil {
		t.Fatalf("seed variant: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO warehouses(saby_id, name, city, address)
		VALUES ($1, 'CI load warehouse', 'CI', 'CI') RETURNING id
	`, "ci-load-"+suffix).Scan(&warehouseID); err != nil {
		t.Fatalf("seed warehouse: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO inventory(warehouse_id, variant_id, available_qty)
		VALUES ($1, $2, 3)
	`, warehouseID, variantID); err != nil {
		t.Fatalf("seed inventory: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM stock_movements WHERE order_id IN (SELECT id FROM orders WHERE email LIKE $1)`, emailPattern)
		_, _ = pool.Exec(ctx, `DELETE FROM consent_events WHERE order_id IN (SELECT id FROM orders WHERE email LIKE $1)`, emailPattern)
		_, _ = pool.Exec(ctx, `DELETE FROM procurement_requests WHERE customer_order_id IN (SELECT id FROM orders WHERE email LIKE $1)`, emailPattern)
		_, _ = pool.Exec(ctx, `DELETE FROM orders WHERE email LIKE $1`, emailPattern)
		_, _ = pool.Exec(ctx, `DELETE FROM outbox WHERE recipient LIKE $1`, emailPattern)
		_, _ = pool.Exec(ctx, "DELETE FROM products WHERE id=$1", productID)
		_, _ = pool.Exec(ctx, "DELETE FROM warehouses WHERE id=$1", warehouseID)
	}()

	service := NewService(pool, nil, liveOrderNotifier{}, nil, quietLogger())
	const buyers = 8
	errorsFound := make(chan error, buyers)
	var wait sync.WaitGroup
	for buyer := 0; buyer < buyers; buyer++ {
		wait.Add(1)
		go func(buyer int) {
			defer wait.Done()
			_, err := service.Create(ctx, CreateInput{
				Customer: CustomerInput{
					Name: "CI Load", Phone: fmt.Sprintf("+7900001%04d", buyer),
					Email: fmt.Sprintf("ci-load-%s-%d@example.invalid", suffix, buyer),
				},
				Delivery: "pickup", Items: []ItemInput{{ID: sku, Quantity: 1}},
				Consent: true, PaymentMethod: payment.MethodOnDelivery,
			})
			if err != nil {
				errorsFound <- err
			}
		}(buyer)
	}
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Errorf("concurrent checkout: %v", err)
	}
	if t.Failed() {
		return
	}

	var orders, itemReservations, inventoryReservations, preorders int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT purchase.id)::integer,
		       COALESCE(SUM(item.reserved_qty), 0)::integer,
		       (SELECT reserved_qty FROM inventory WHERE warehouse_id=$2 AND variant_id=$3),
		       COUNT(*) FILTER (WHERE item.is_preorder=1)::integer
		FROM orders purchase
		JOIN order_items item ON item.order_id=purchase.id
		WHERE purchase.email LIKE $1
	`, emailPattern, warehouseID, variantID).Scan(&orders, &itemReservations, &inventoryReservations, &preorders); err != nil {
		t.Fatal(err)
	}
	if orders != buyers || itemReservations != 3 || inventoryReservations != 3 || preorders != buyers-3 {
		t.Fatalf("oversell guard failed: orders=%d item_reserved=%d inventory_reserved=%d preorders=%d", orders, itemReservations, inventoryReservations, preorders)
	}
}
