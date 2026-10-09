package order

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/avpavlo8/ficusin-store/backend/internal/payment"
	"github.com/jackc/pgx/v5/pgxpool"
)

type liveOrderNotifier struct{}

func (liveOrderNotifier) SendOrder(context.Context, integration.TelegramOrder) error { return nil }

// TestCommerceLifecycleOnLiveDatabase is the release-level proof that the
// migrated schema and the real order service still agree. It deliberately
// exercises the atomic facts that cannot be inferred from a 201 response:
// no reservation, immutable order snapshots, consent, outbox and idempotent
// cancellation.
func TestCommerceLifecycleOnLiveDatabase(t *testing.T) {
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
	sabyID := "ci-commerce-saby-" + suffix
	email := "commerce-" + suffix + "@example.invalid"
	phone := "+7000" + suffix[len(suffix)-7:]
	var productID, variantID, warehouseID int64
	var sku string
	if err := pool.QueryRow(ctx, `
		INSERT INTO products(name, slug, short_description, description, search_text, status, category_id)
		SELECT 'CI commerce product', $1, 'Release test', 'Release test product',
		       'ci commerce product', 'published', id
		FROM categories WHERE slug='accessories'
		RETURNING id
	`, "ci-commerce-"+suffix).Scan(&productID); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE products SET saby_fields=ARRAY['stock']::text[] WHERE id=$1`, productID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,name,balance) VALUES($1,'CI commerce Saby',5)`, sabyID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO product_variants(product_id, saby_id, label, base_price_minor)
		VALUES ($1, $2, 'CI variant', 149000)
		RETURNING id, sku
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
		VALUES ($1, $2, 5)
	`, warehouseID, variantID); err != nil {
		t.Fatalf("seed inventory: %v", err)
	}
	stockTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stockTx.Rollback(ctx) }()
	stockItem := purchasableItem{VariantID: variantID, Quantity: 2}
	if preorder, err := needsPreorder(ctx, stockTx, stockItem); err != nil || preorder {
		t.Fatalf("fresh Saby stock unavailable: preorder=%v err=%v", preorder, err)
	}
	if _, err := stockTx.Exec(ctx, `UPDATE inventory SET synced_at=CURRENT_TIMESTAMP-INTERVAL '3 hours' WHERE variant_id=$1`, variantID); err != nil {
		t.Fatal(err)
	}
	if preorder, err := needsPreorder(ctx, stockTx, stockItem); err != nil || !preorder {
		t.Fatalf("stale Saby stock must require review: preorder=%v err=%v", preorder, err)
	}
	if _, err := stockTx.Exec(ctx, `UPDATE inventory SET synced_at=CURRENT_TIMESTAMP WHERE variant_id=$1`, variantID); err != nil {
		t.Fatal(err)
	}
	if _, err := stockTx.Exec(ctx, `UPDATE products SET saby_fields=ARRAY[]::text[] WHERE id=$1`, productID); err != nil {
		t.Fatal(err)
	}
	if preorder, err := needsPreorder(ctx, stockTx, stockItem); err != nil || !preorder {
		t.Fatalf("unconfirmed stock must require review: preorder=%v err=%v", preorder, err)
	}
	if err := stockTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	var orderID, preorderID int64
	defer func() {
		if preorderID != 0 {
			_, _ = pool.Exec(ctx, "DELETE FROM consent_events WHERE order_id=$1", preorderID)
			_, _ = pool.Exec(ctx, "DELETE FROM orders WHERE id=$1", preorderID)
		}
		if orderID != 0 {
			_, _ = pool.Exec(ctx, "DELETE FROM stock_movements WHERE order_id=$1", orderID)
			_, _ = pool.Exec(ctx, "DELETE FROM consent_events WHERE order_id=$1", orderID)
			_, _ = pool.Exec(ctx, "DELETE FROM orders WHERE id=$1", orderID)
		}
		_, _ = pool.Exec(ctx, "DELETE FROM outbox WHERE recipient=$1", email)
		_, _ = pool.Exec(ctx, "DELETE FROM products WHERE id=$1", productID)
		_, _ = pool.Exec(ctx, "DELETE FROM saby_nomenclature WHERE saby_id=$1", sabyID)
	}()

	service := NewService(pool, nil, liveOrderNotifier{}, nil, quietLogger())
	created, err := service.Create(ctx, CreateInput{
		Customer: CustomerInput{
			Name: "CI Commerce", Phone: phone, Email: email,
		},
		Delivery:      "pickup",
		Items:         []ItemInput{{ID: sku, Quantity: 2}},
		PaymentMethod: payment.MethodOnDelivery,
		Consent:       true,
		ClientIP:      "127.0.0.1",
		UserAgent:     "ficusin-release-test",
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	if created.OrderNumber == "" || created.PaymentStatus != payment.StatusOnDelivery || created.Total != 2980 {
		t.Fatalf("unexpected created order: %#v", created)
	}

	var facts string
	if err := pool.QueryRow(ctx, `
		SELECT o.id,
		       o.payment_status || ':' || o.total::TEXT || ':' ||
		       oi.sku || ':' || oi.quantity::TEXT || ':' || oi.reserved_qty::TEXT || ':' ||
		       i.reserved_qty::TEXT || ':' ||
		       (SELECT COUNT(*) FROM consent_events c WHERE c.order_id=o.id)::TEXT || ':' ||
		       (SELECT COUNT(*) FROM outbox x WHERE x.recipient=$2)::TEXT || ':' ||
		       (SELECT COUNT(*) FROM stock_movements m WHERE m.order_id=o.id AND m.kind='reserve')::TEXT || ':' ||
		       COALESCE((SELECT SUM(quantity) FROM stock_movements m WHERE m.order_id=o.id AND m.kind='reserve'),0)::TEXT
		FROM orders o
		JOIN order_items oi ON oi.order_id=o.id
		JOIN inventory i ON i.variant_id=oi.variant_id
		WHERE o.order_number=$1
	`, created.OrderNumber, email).Scan(&orderID, &facts); err != nil {
		t.Fatalf("read order facts: %v", err)
	}
	if facts != payment.StatusOnDelivery+":2980.00:"+sku+":2:0:0:1:1:0:0" {
		t.Fatalf("commerce transaction is incomplete: %s", facts)
	}

	worker := &ExpiryWorker{pool: pool, logger: quietLogger()}
	if err := worker.cancel(ctx, orderID); err != nil {
		t.Fatalf("cancel order: %v", err)
	}
	if err := worker.cancel(ctx, orderID); err != nil {
		t.Fatalf("repeat cancellation must be safe: %v", err)
	}
	var cancelled string
	if err := pool.QueryRow(ctx, `
		SELECT o.status || ':' || o.payment_status || ':' ||
		       (o.stock_released_at IS NOT NULL)::TEXT || ':' || i.reserved_qty::TEXT || ':' ||
		       (SELECT COUNT(*) FROM stock_movements m WHERE m.order_id=o.id AND m.kind='release')::TEXT
		FROM orders o
		JOIN order_items oi ON oi.order_id=o.id
		JOIN inventory i ON i.variant_id=oi.variant_id
		WHERE o.id=$1
	`, orderID).Scan(&cancelled); err != nil {
		t.Fatalf("read cancellation facts: %v", err)
	}
	if cancelled != "cancelled:cancelled:true:0:0" {
		t.Fatalf("cancellation is not atomic/idempotent: %s", cancelled)
	}
	if _, err := pool.Exec(ctx, `UPDATE inventory SET synced_at=CURRENT_TIMESTAMP-INTERVAL '3 hours' WHERE variant_id=$1`, variantID); err != nil {
		t.Fatal(err)
	}
	preorder, err := service.Create(ctx, CreateInput{
		Customer: CustomerInput{Name: "CI Preorder", Phone: phone, Email: email},
		Delivery: "pickup", Items: []ItemInput{{ID: sku, Quantity: 1}},
		Consent: true, ClientIP: "127.0.0.1", UserAgent: "ficusin-release-test",
	})
	if err != nil {
		t.Fatalf("create preorder without payment choice: %v", err)
	}
	if !preorder.HasPreorder || preorder.PaymentStatus != payment.StatusPending {
		t.Fatalf("preorder must wait for manager without payment: %#v", preorder)
	}
	var preorderMethod string
	if err := pool.QueryRow(ctx, `SELECT id, payment_method FROM orders WHERE order_number=$1`, preorder.OrderNumber).Scan(&preorderID, &preorderMethod); err != nil {
		t.Fatal(err)
	}
	if preorderMethod != payment.MethodManager {
		t.Fatalf("preorder payment method = %q, want manager confirmation", preorderMethod)
	}
}
