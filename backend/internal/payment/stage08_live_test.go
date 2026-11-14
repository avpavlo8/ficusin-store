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

func TestStage08ShipmentPaymentIsIdempotentAndHasExactReceipt(t *testing.T) {
	databaseURL := os.Getenv("CRM_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("CRM_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	unique := time.Now().UnixNano()
	var customerID, orderID, productID, variantID, itemID, offerID int64
	var sku string
	_ = pool.QueryRow(ctx, `SELECT id FROM customers WHERE email='crm-owner@example.invalid'`).Scan(&customerID)
	_ = pool.QueryRow(ctx, `SELECT p.id,pv.id,pv.sku FROM products p JOIN product_variants pv ON pv.product_id=p.id WHERE p.slug='crm-stage07-a'`).Scan(&productID, &variantID, &sku)
	if err := pool.QueryRow(ctx, `INSERT INTO orders(order_number,customer_id,customer_name,phone,email,address,delivery_method,delivery_fee,subtotal,total,payment_method,payment_status,status,has_preorder) VALUES($1,$2,'Stage 08','+70000000000','stage08@example.invalid','Рязань','pickup',0,2290,2290,'online','pending','confirmed',1) RETURNING id`, fmt.Sprintf("CRM-S08-P-%d", unique), customerID).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id=$1`, orderID) }()
	_ = pool.QueryRow(ctx, `INSERT INTO order_items(order_id,product_id,variant_id,sku,product_name,variant_label,variant_snapshot,unit_price,quantity,is_preorder,reserved_qty) VALUES($1,$2,$3,$4,'Stage 08 plant','D17','{}',2290,1,1,0) RETURNING id`, orderID, productID, variantID, sku).Scan(&itemID)
	token := fmt.Sprintf("stage08-pay-%d", unique)
	_ = pool.QueryRow(ctx, `INSERT INTO shipment_offers(order_id,public_token,order_revision,status,delivery_method,address_snapshot,subtotal,delivery_fee,total,notified_at,expires_at,created_by) VALUES($1,$2,1,'offered','pickup','Рязань',2290,0,2290,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP+INTERVAL '48 hours',$3) RETURNING id`, orderID, token, customerID).Scan(&offerID)
	_, _ = pool.Exec(ctx, `INSERT INTO shipment_offer_items(shipment_offer_id,order_item_id,variant_id,sku,product_name,unit_price,quantity) VALUES($1,$2,$3,$4,'Stage 08 plant',2290,1)`, offerID, itemID, variantID, sku)
	var warehouseID int64
	if err = pool.QueryRow(ctx, `SELECT id FROM warehouses WHERE saby_id='saby-ryazan-main' AND is_active=1`).Scan(&warehouseID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM inventory WHERE warehouse_id=$1 AND variant_id=$2`, warehouseID, variantID)
	}()
	if _, err = pool.Exec(ctx, `INSERT INTO inventory(warehouse_id,variant_id,available_qty,reserved_qty) VALUES($1,$2,5,0)`, warehouseID, variantID); err != nil {
		t.Fatal(err)
	}
	provider := &stage13TimeoutProvider{}
	service := NewService(pool, provider, "https://ficusin.example.invalid", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err = service.StartShipmentOffer(ctx, token, customerID); err == nil {
		t.Fatal("expected ambiguous provider timeout")
	}
	originalKey := provider.request.IdempotenceKey
	// The order also contains plants outside this shipment. Their shortage
	// must not block recovery of the payment for the available shipment item.
	if _, err = pool.Exec(ctx, `INSERT INTO order_items(order_id,product_id,variant_id,sku,product_name,variant_label,variant_snapshot,unit_price,quantity,is_preorder,reserved_qty) VALUES($1,$2,$3,$4,'Other plants','D17','{}',2290,10,1,0)`, orderID, productID, variantID, sku); err != nil {
		t.Fatal(err)
	}
	var unknownPaymentID int64
	if err = pool.QueryRow(ctx, `SELECT id FROM payments WHERE shipment_offer_id=$1 AND status='pending'`, offerID).Scan(&unknownPaymentID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE inventory SET synced_at=CURRENT_TIMESTAMP-INTERVAL '3 hours' WHERE warehouse_id=$1 AND variant_id=$2`, warehouseID, variantID); err != nil {
		t.Fatal(err)
	}
	if _, err = service.RecoverUnknown(ctx, unknownPaymentID); err == nil || provider.creates != 1 {
		t.Fatalf("stale shipment stock allowed recovery: err=%v creates=%d", err, provider.creates)
	}
	if _, err = pool.Exec(ctx, `UPDATE inventory SET synced_at=CURRENT_TIMESTAMP WHERE warehouse_id=$1 AND variant_id=$2`, warehouseID, variantID); err != nil {
		t.Fatal(err)
	}
	if _, err = service.RecoverUnknown(ctx, unknownPaymentID); err != nil {
		t.Fatalf("mixed order blocked recovery of available shipment: %v", err)
	}
	first, err := service.StartShipmentOffer(ctx, token, customerID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.StartShipmentOffer(ctx, token, customerID)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || provider.creates != 2 {
		t.Fatalf("not idempotent: %q %q creates=%d", first, second, provider.creates)
	}
	if provider.request.Amount != 2290 || len(provider.request.Items) != 1 || provider.request.Items[0].Name != "Stage 08 plant" {
		t.Fatalf("invalid receipt: %+v", provider.request)
	}
	if provider.request.IdempotenceKey != originalKey {
		t.Fatal("timeout changed payment idempotence key")
	}
	var reserved int
	if err = pool.QueryRow(ctx, `SELECT reserved_qty FROM order_items WHERE id=$1`, itemID).Scan(&reserved); err != nil || reserved != 0 {
		t.Fatalf("checkout must not reserve stock: %d err=%v", reserved, err)
	}
	provider.status = integration.Payment{ID: "ci-provider-payment", Status: "succeeded", Paid: true, Amount: 2290}
	if err := service.SyncOutstanding(ctx, "ci-provider-payment"); err != nil {
		t.Fatal(err)
	}
	var status string
	_ = pool.QueryRow(ctx, `SELECT status FROM shipment_offers WHERE id=$1`, offerID).Scan(&status)
	if status != "paid" {
		t.Fatalf("offer status=%s", status)
	}
	if _, err = pool.Exec(ctx, `UPDATE shipment_offers SET status='shipped' WHERE id=$1`, offerID); err != nil {
		t.Fatal(err)
	}
	if err = service.SyncOutstanding(ctx, "ci-provider-payment"); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT status FROM shipment_offers WHERE id=$1`, offerID).Scan(&status); err != nil || status != "shipped" {
		t.Fatalf("repeated webhook rewound fulfillment: %s %v", status, err)
	}
}

func TestCancelledOrderRejectsShipmentOfferPayment(t *testing.T) {
	databaseURL := os.Getenv("CRM_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("CRM_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	unique := time.Now().UnixNano()
	var customerID, orderID, offerID, productID, variantID, itemID, warehouseID int64
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	exec := func(sql string, args ...any) { t.Helper(); _, err := pool.Exec(ctx, sql, args...); must(err) }
	must(pool.QueryRow(ctx, `SELECT id FROM customers WHERE email='crm-owner@example.invalid'`).Scan(&customerID))
	must(pool.QueryRow(ctx, `SELECT id FROM warehouses WHERE saby_id='saby-ryazan-main' AND is_active=1`).Scan(&warehouseID))
	// Own fresh stock: shared CRM inventory must not accidentally determine
	// whether the offer is valid or be modified by the positive control.
	sabyID := fmt.Sprintf("cancelled-offer-stock-%d", unique)
	exec(`INSERT INTO saby_nomenclature(saby_id,name) VALUES($1,'Cancellation guard plant')`, sabyID)
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM saby_nomenclature WHERE saby_id=$1`, sabyID) }()
	must(pool.QueryRow(ctx, `INSERT INTO products(name,slug,saby_id,saby_fields) VALUES('Cancellation guard plant',$1,$2,ARRAY['stock']) RETURNING id`, sabyID, sabyID).Scan(&productID))
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, productID) }()
	var sku string
	must(pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,saby_id,label,base_price_minor) VALUES($1,$2,'D17',10000) RETURNING id,sku`, productID, sabyID).Scan(&variantID, &sku))
	exec(`INSERT INTO inventory(warehouse_id,variant_id,available_qty,reserved_qty,synced_at) VALUES($1,$2,5,0,CURRENT_TIMESTAMP)`, warehouseID, variantID)
	must(pool.QueryRow(ctx, `INSERT INTO orders(order_number,customer_id,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,payment_method,payment_status,status)
 VALUES($1,$2,'Cancellation guard buyer','+70000000000','cancelled-offer@example.invalid','pickup',0,100,100,'online','pending','confirmed') RETURNING id`, fmt.Sprintf("CRM-CANCEL-OFFER-%d", unique), customerID).Scan(&orderID))
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id=$1`, orderID) }()
	must(pool.QueryRow(ctx, `INSERT INTO order_items(order_id,product_id,variant_id,sku,product_name,variant_label,variant_snapshot,unit_price,quantity,is_preorder,reserved_qty)
 VALUES($1,$2,$3,$4,'Cancellation guard plant','D17','{}',100,1,1,0) RETURNING id`, orderID, productID, variantID, sku).Scan(&itemID))
	token := fmt.Sprintf("cancelled-offer-%d", unique)
	must(pool.QueryRow(ctx, `INSERT INTO shipment_offers(order_id,public_token,order_revision,status,delivery_method,subtotal,total,expires_at,created_by)
 VALUES($1,$2,1,'offered','pickup',100,100,CURRENT_TIMESTAMP+INTERVAL '48 hours',$3) RETURNING id`, orderID, token, customerID).Scan(&offerID))
	exec(`INSERT INTO shipment_offer_items(shipment_offer_id,order_item_id,variant_id,sku,product_name,unit_price,quantity)
 VALUES($1,$2,$3,$4,'Cancellation guard plant',100,1)`, offerID, itemID, variantID, sku)
	provider := &livePaymentProvider{}
	service := NewService(pool, provider, "https://ficusin.example.invalid", slog.New(slog.NewTextHandler(io.Discard, nil)))
	// The identical open offer must create a local payment and reach only the
	// fake provider. Empty items, stale stock and invalid totals cannot explain
	// the cancellation error checked below.
	url, err := service.StartShipmentOffer(ctx, token, customerID)
	if err != nil || url == "" || provider.creates != 1 || provider.request.Amount != 100 || len(provider.request.Items) != 1 || provider.request.Items[0].Quantity != 1 {
		t.Fatalf("valid open offer did not reach payment creation: url=%q err=%v creates=%d request=%+v", url, err, provider.creates, provider.request)
	}
	var payments int
	must(pool.QueryRow(ctx, `SELECT count(*) FROM payments WHERE order_id=$1 AND shipment_offer_id=$2`, orderID, offerID).Scan(&payments))
	if payments != 1 {
		t.Fatalf("positive control created %d local payments", payments)
	}
	exec(`DELETE FROM payments WHERE order_id=$1`, orderID)
	exec(`UPDATE shipment_offers SET status='offered',checkout_locked_until=NULL WHERE id=$1`, offerID)
	exec(`UPDATE orders SET status='cancelled',payment_status='cancelled' WHERE id=$1`, orderID)
	provider = &livePaymentProvider{}
	service = NewService(pool, provider, "https://ficusin.example.invalid", slog.New(slog.NewTextHandler(io.Discard, nil)))
	const closedOrderMessage = "заказ закрыт — оплатить отправку нельзя"
	if _, err = service.StartShipmentOffer(ctx, token, customerID); err == nil || err.Error() != closedOrderMessage {
		t.Fatalf("cancelled order did not hit the public parent-status guard: %v", err)
	}
	if _, err = service.reserveOfferAndCreatePayment(ctx, offerID, orderID, "cancelled-order-test", 100); err == nil || err.Error() != closedOrderMessage {
		t.Fatalf("cancelled order did not hit the locked transaction parent-status guard: %v", err)
	}
	if provider.creates != 0 {
		t.Fatalf("provider received %d creates after cancellation", provider.creates)
	}
	must(pool.QueryRow(ctx, `SELECT count(*) FROM payments WHERE order_id=$1`, orderID).Scan(&payments))
	if payments != 0 {
		t.Fatalf("cancelled order created %d local payments", payments)
	}
	var available, reserved, itemReserved int
	must(pool.QueryRow(ctx, `SELECT available_qty,reserved_qty FROM inventory WHERE warehouse_id=$1 AND variant_id=$2`, warehouseID, variantID).Scan(&available, &reserved))
	must(pool.QueryRow(ctx, `SELECT reserved_qty FROM order_items WHERE id=$1`, itemID).Scan(&itemReserved))
	if available != 5 || reserved != 0 || itemReserved != 0 {
		t.Fatalf("payment attempts changed stock: available=%d reserved=%d itemReserved=%d", available, reserved, itemReserved)
	}
}

type stage13TimeoutProvider struct{ livePaymentProvider }

func (provider *stage13TimeoutProvider) CreatePayment(ctx context.Context, request integration.PaymentRequest) (integration.Payment, error) {
	result, err := provider.livePaymentProvider.CreatePayment(ctx, request)
	if provider.creates == 1 {
		return integration.Payment{}, fmt.Errorf("%w: simulated response timeout after provider acceptance", integration.ErrPaymentOutcomeUnknown)
	}
	return result, err
}
