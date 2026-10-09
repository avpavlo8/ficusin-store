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
// transactions through checkout. Orders no longer reserve any stock.
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
	sabyID := "ci-load-saby-" + suffix
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
	if _, err := pool.Exec(ctx, `UPDATE products SET saby_fields=ARRAY['stock']::text[] WHERE id=$1`, productID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,name,balance) VALUES($1,'CI load Saby',3)`, sabyID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO product_variants(product_id, saby_id, label, base_price_minor)
		VALUES ($1, $2, 'CI load variant', 10000) RETURNING id, sku
	`, productID, sabyID).Scan(&variantID, &sku); err != nil {
		t.Fatalf("seed variant: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO warehouses(saby_id, name, city, address, is_active)
		VALUES ('saby-ryazan-main', 'Основной склад', 'Рязань', 'CI', 1) ON CONFLICT(saby_id) DO NOTHING`); err != nil {
		t.Fatalf("seed warehouse: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM warehouses WHERE saby_id='saby-ryazan-main' AND is_active=1`).Scan(&warehouseID); err != nil {
		t.Fatalf("find Saby warehouse: %v", err)
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
		_, _ = pool.Exec(ctx, "DELETE FROM saby_nomenclature WHERE saby_id=$1", sabyID)
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
	if orders != buyers || itemReservations != 0 || inventoryReservations != 0 || preorders != 0 {
		t.Fatalf("checkout reserved stock or marked an available product as preorder: orders=%d item_reserved=%d inventory_reserved=%d preorders=%d", orders, itemReservations, inventoryReservations, preorders)
	}
}
