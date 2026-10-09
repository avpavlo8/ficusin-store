package payment

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/jackc/pgx/v5/pgxpool"
)

type livePaymentProvider struct {
	creates int
	refunds int
	request integration.PaymentRequest
	status  integration.Payment
}

func (*livePaymentProvider) Configured() bool { return true }
func (provider *livePaymentProvider) CreatePayment(_ context.Context, request integration.PaymentRequest) (integration.Payment, error) {
	provider.creates++
	provider.request = request
	return integration.Payment{ID: "ci-provider-payment", Status: StatusPending, Amount: request.Amount, ConfirmationURL: "https://payments.example.invalid/ci"}, nil
}
func (provider *livePaymentProvider) FetchPayment(context.Context, string) (integration.Payment, error) {
	return provider.status, nil
}
func (provider *livePaymentProvider) Refund(_ context.Context, _ string, _ float64, key string) error {
	if key == "" {
		return fmt.Errorf("refund has no idempotence key")
	}
	provider.refunds++
	return nil
}

// TestPaymentLifecycleOnLiveDatabase proves the money state machine against
// the fully migrated schema without contacting YooKassa or moving real money.
func TestPaymentLifecycleOnLiveDatabase(t *testing.T) {
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
	sabyID := "ci-payment-saby-" + suffix

	var productID, variantID, warehouseID, orderID int64
	var sku string
	if err := pool.QueryRow(ctx, `
		INSERT INTO products(name, slug, status, category_id)
		SELECT 'CI payment product', $1, 'published', id
		FROM categories WHERE slug='accessories' RETURNING id
	`, "ci-payment-"+suffix).Scan(&productID); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE products SET saby_fields=ARRAY['stock']::text[] WHERE id=$1`, productID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,name,balance) VALUES($1,'CI payment Saby',1)`, sabyID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO product_variants(product_id, saby_id, label, base_price_minor)
		VALUES ($1, $2, 'CI payment variant', 149000) RETURNING id, sku
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
		VALUES ($1, $2, 1)
	`, warehouseID, variantID); err != nil {
		t.Fatalf("seed inventory: %v", err)
	}
	orderNumber := "CI-PAY-" + suffix
	if err := pool.QueryRow(ctx, `
		INSERT INTO orders(order_number, customer_name, phone, email, delivery_method,
		                   delivery_fee, subtotal, total, payment_method, payment_status)
		VALUES ($1, 'CI Payment', '+70000000000', 'payment@example.invalid', 'pickup',
		        0, 1490, 1490, 'online', 'pending') RETURNING id
	`, orderNumber).Scan(&orderID); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO order_items(order_id, product_id, variant_id, sku, product_name,
		                        variant_label, unit_price, quantity, reserved_qty)
		VALUES ($1, $2, $3, $4, 'CI payment product', 'CI payment variant', 1490, 1, 0)
	`, orderID, productID, variantID, sku); err != nil {
		t.Fatalf("seed order item: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM orders WHERE id=$1", orderID)
		_, _ = pool.Exec(ctx, "DELETE FROM products WHERE id=$1", productID)
		_, _ = pool.Exec(ctx, "DELETE FROM saby_nomenclature WHERE saby_id=$1", sabyID)
	}()

	provider := &livePaymentProvider{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := NewService(pool, provider, "https://ficusin.example.invalid", logger)
	if _, err := pool.Exec(ctx, `UPDATE inventory SET synced_at=CURRENT_TIMESTAMP-INTERVAL '3 hours' WHERE variant_id=$1`, variantID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Start(ctx, orderNumber); err == nil || provider.creates != 0 {
		t.Fatalf("stale Saby stock opened payment: err=%v creates=%d", err, provider.creates)
	}
	if _, err := pool.Exec(ctx, `UPDATE inventory SET synced_at=CURRENT_TIMESTAMP WHERE variant_id=$1`, variantID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE products SET saby_fields=ARRAY[]::text[] WHERE id=$1`, productID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Start(ctx, orderNumber); err == nil || provider.creates != 0 {
		t.Fatalf("unconfirmed stock opened payment: err=%v creates=%d", err, provider.creates)
	}
	if _, err := pool.Exec(ctx, `UPDATE products SET saby_fields=ARRAY['stock']::text[] WHERE id=$1`, productID); err != nil {
		t.Fatal(err)
	}
	firstURL, err := service.Start(ctx, orderNumber)
	if err != nil {
		t.Fatalf("start payment: %v", err)
	}
	secondURL, err := service.Start(ctx, orderNumber)
	if err != nil {
		t.Fatalf("repeat payment: %v", err)
	}
	if firstURL != secondURL || provider.creates != 1 {
		t.Fatalf("payment is not idempotent: urls=%q/%q creates=%d", firstURL, secondURL, provider.creates)
	}
	if provider.request.Amount != 1490 || len(provider.request.Items) != 1 || provider.request.Items[0].Name != "CI payment product" {
		t.Fatalf("provider received an invalid amount/receipt: %#v", provider.request)
	}

	provider.status = integration.Payment{ID: "ci-provider-payment", Status: "succeeded", Paid: true, Amount: 1490}
	if err := service.Sync(ctx, "ci-provider-payment"); err != nil {
		t.Fatalf("sync paid payment: %v", err)
	}
	var state string
	if err := pool.QueryRow(ctx, `
		SELECT o.payment_status || ':' || (o.paid_at IS NOT NULL)::TEXT || ':' ||
		       p.status || ':' || (p.paid_at IS NOT NULL)::TEXT
		FROM orders o JOIN payments p ON p.order_id=o.id WHERE o.id=$1
	`, orderID).Scan(&state); err != nil {
		t.Fatalf("read paid state: %v", err)
	}
	if state != "paid:true:paid:true" {
		t.Fatalf("paid state is inconsistent: %s", state)
	}

	if err := service.Refund(ctx, orderID); err != nil {
		t.Fatalf("refund payment: %v", err)
	}
	if provider.refunds != 1 {
		t.Fatalf("refund provider calls = %d, want 1", provider.refunds)
	}
	if err := pool.QueryRow(ctx, `
		SELECT o.payment_status || ':' || p.status
		FROM orders o JOIN payments p ON p.order_id=o.id WHERE o.id=$1
	`, orderID).Scan(&state); err != nil {
		t.Fatalf("read refunded state: %v", err)
	}
	if state != "refunded:refunded" {
		t.Fatalf("refunded state is inconsistent: %s", state)
	}
}
