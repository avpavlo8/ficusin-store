package order

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/catalog"
	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/avpavlo8/ficusin-store/backend/internal/settings"
	"github.com/jackc/pgx/v5/pgxpool"
)

type checkoutShippingSettings struct{ enabled bool }

func (s *checkoutShippingSettings) Number(key string) int {
	if key == settings.CDEKOrdersEnabled && s.enabled {
		return 1
	}
	return 0
}
func (s *checkoutShippingSettings) Enabled(key string) bool {
	return key == settings.CDEKOrdersEnabled && s.enabled
}
func (*checkoutShippingSettings) Value(string) string { return "Checkout sender" }

type checkoutCDEK struct {
	requestMu sync.Mutex
	*stage13CDEK
	requests    []integration.ShipmentRequest
	cancelled   []string
	beforeQuote func(context.Context)
}

func (c *checkoutCDEK) GetOffices(context.Context, int) ([]integration.CDEKOffice, error) {
	return []integration.CDEKOffice{{Code: "CHECKOUT-PVZ", Location: integration.CDEKLocation{City: "Москва", Address: "Test PVZ"}}}, nil
}
func (c *checkoutCDEK) CalculatePVZ(ctx context.Context, _ int, _ integration.Parcel) ([]integration.CDEKQuote, error) {
	if c.beforeQuote != nil {
		c.beforeQuote(ctx)
	}
	return []integration.CDEKQuote{{TariffCode: 136, Price: 450}}, nil
}
func (c *checkoutCDEK) CreateOrder(ctx context.Context, r integration.ShipmentRequest) (integration.Shipment, error) {
	c.requestMu.Lock()
	c.requests = append(c.requests, r)
	c.requestMu.Unlock()
	return c.stage13CDEK.CreateOrder(ctx, r)
}
func (c *checkoutCDEK) CancelOrder(_ context.Context, uuid string) error {
	c.cancelled = append(c.cancelled, uuid)
	return nil
}

// Exercises the real checkout transaction, worker SQL and cancellation against
// migrated PostgreSQL. Only the carrier is replaced; no real parcels are made.
func TestCheckoutCDEKBeforePaymentOnLiveDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	suffix := fmt.Sprint(time.Now().UnixNano())
	sabyID := "checkout-cdek-" + suffix
	email := sabyID + "@example.invalid"
	var productID, variantID, warehouseID, customerID int64
	var sku string
	if err := pool.QueryRow(ctx, `INSERT INTO products(name,slug,status,category_id,saby_fields) SELECT 'Checkout CDEK test',$1,'published',id,ARRAY['stock']::text[] FROM categories WHERE slug='accessories' RETURNING id`, sabyID).Scan(&productID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM orders WHERE email=$1`, email)
		_, _ = pool.Exec(ctx, `DELETE FROM outbox WHERE recipient=$1`, email)
		_, _ = pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, customerID)
		_, _ = pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, productID)
		_, _ = pool.Exec(ctx, `DELETE FROM saby_nomenclature WHERE saby_id=$1`, sabyID)
	}()
	if _, err := pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,name,balance) VALUES($1,'Checkout plant',10)`, sabyID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,saby_id,label,base_price_minor) VALUES($1,$2,'D12',149000) RETURNING id,sku`, productID, sabyID).Scan(&variantID, &sku); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO warehouses(saby_id,name,city,address,is_active) VALUES('saby-ryazan-main','Основной склад','Рязань','CI',1) ON CONFLICT(saby_id) DO UPDATE SET is_active=1 RETURNING id`).Scan(&warehouseID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO inventory(warehouse_id,variant_id,available_qty,reserved_qty,synced_at) VALUES($1,$2,10,0,CURRENT_TIMESTAMP)`, warehouseID, variantID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO variant_attribute_values(variant_id,attribute_id,value) SELECT $1,d.id,to_jsonb(v.n) FROM attribute_definitions d JOIN (VALUES('package_length_cm',40),('package_width_cm',20),('package_height_cm',20),('package_weight_grams',1500))v(code,n) ON v.code=d.code ON CONFLICT(variant_id,attribute_id) DO UPDATE SET value=EXCLUDED.value`, variantID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,last_name,consent_at) VALUES($1,$2,'','Александр','Павловский',CURRENT_TIMESTAMP) RETURNING id`, email, "ci-mail-"+suffix).Scan(&customerID); err != nil {
		t.Fatal(err)
	}
	shop := &checkoutShippingSettings{enabled: true}
	client := &checkoutCDEK{stage13CDEK: &stage13CDEK{byNumber: map[string]integration.Shipment{}, byUUID: map[string]integration.Shipment{}}}
	worker := NewShippingWorker(pool, client, shop, nil, quietLogger())
	wakes := 0
	service := NewService(pool, client, liveOrderNotifier{}, shop, quietLogger()).WithShipmentWake(func() { wakes++; worker.Wake() })
	makeOrder := func(repack bool) int64 {
		t.Helper()
		result, err := service.Create(ctx, CreateInput{CustomerID: &customerID, Customer: CustomerInput{Name: "Павловский Александр", Phone: "+70000000000", Email: email}, Delivery: "cdek", Items: []ItemInput{{ID: sku, Quantity: 1}}, CDEK: CDEKInput{CityCode: 44, OfficeCode: "CHECKOUT-PVZ", TariffCode: 136, Repack: repack}, PaymentMethod: "online", OnlinePaymentReady: true, Consent: true, OfferAccepted: true, ClientIP: "127.0.0.1"})
		if err != nil {
			t.Fatal(err)
		}
		var id int64
		if err := pool.QueryRow(ctx, `SELECT id FROM orders WHERE order_number=$1`, result.OrderNumber).Scan(&id); err != nil {
			t.Fatal(err)
		}
		var confirmation string
		if err := pool.QueryRow(ctx, `SELECT body FROM outbox WHERE subject=$1 ORDER BY id DESC LIMIT 1`, "Заказ "+result.OrderNumber+" принят").Scan(&confirmation); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(confirmation, "Здравствуйте, Александр!") || strings.Contains(confirmation, "Здравствуйте, Павловский!") {
			t.Fatal("confirmation uses contact surname instead of structured given name")
		}
		return id
	}
	// With one connection, even a SELECT during the carrier callback would
	// time out if checkout still owned a transaction. Also change the package
	// and price while quoting to exercise the second database read.
	client.beforeQuote = func(ctx context.Context) {
		checkCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		if _, err := pool.Exec(checkCtx, `UPDATE product_variants SET base_price_minor=159000 WHERE id=$1`, variantID); err != nil {
			t.Fatalf("carrier call holds database connection: %v", err)
		}
		if _, err := pool.Exec(checkCtx, `UPDATE variant_attribute_values SET value='50'::jsonb WHERE variant_id=$1 AND attribute_id=(SELECT id FROM attribute_definitions WHERE code='package_length_cm')`, variantID); err != nil {
			t.Fatal(err)
		}
	}
	changedID := makeOrder(false)
	var changedPending int
	var changedFee, changedSubtotal float64
	if err := pool.QueryRow(ctx, `SELECT delivery_fee_pending,delivery_fee,subtotal FROM orders WHERE id=$1`, changedID).Scan(&changedPending, &changedFee, &changedSubtotal); err != nil {
		t.Fatal(err)
	}
	if changedPending != 1 || changedFee != 0 || changedSubtotal != 1590 {
		t.Fatalf("stale delivery quote or price: pending=%d fee=%v subtotal=%v", changedPending, changedFee, changedSubtotal)
	}
	client.beforeQuote = nil
	if _, err := pool.Exec(ctx, `UPDATE product_variants SET base_price_minor=149000 WHERE id=$1`, variantID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE variant_attribute_values SET value='40'::jsonb WHERE variant_id=$1 AND attribute_id=(SELECT id FROM attribute_definitions WHERE code='package_length_cm')`, variantID); err != nil {
		t.Fatal(err)
	}
	wakes = 0

	orderID := makeOrder(false)
	var savedFee, savedTotal float64
	if err := pool.QueryRow(ctx, `SELECT delivery_fee,total FROM orders WHERE id=$1`, orderID).Scan(&savedFee, &savedTotal); err != nil {
		t.Fatal(err)
	}
	if savedFee != 462 || savedTotal != 1952 {
		t.Fatalf("declared-value fee missing: delivery=%v total=%v", savedFee, savedTotal)
	}
	if wakes != 1 {
		t.Fatalf("checkout did not wake worker after commit: %d", wakes)
	}
	var pending, flag int
	if err := pool.QueryRow(ctx, `SELECT (payment_status='pending')::int,cdek_checkout_requested FROM orders WHERE id=$1`, orderID).Scan(&pending, &flag); err != nil {
		t.Fatal(err)
	}
	if pending != 1 || flag != 1 {
		t.Fatalf("expected unpaid queued order: pending=%d flag=%d", pending, flag)
	}
	worker.sync(ctx)
	var uuid, status, pay, state string
	if err := pool.QueryRow(ctx, `SELECT cdek_uuid,status,payment_status,cdek_create_state FROM orders WHERE id=$1`, orderID).Scan(&uuid, &status, &pay, &state); err != nil {
		t.Fatal(err)
	}
	if uuid == "" || state != "registered" || status != "new" || pay != "pending" {
		t.Fatalf("before-payment registration failed: uuid=%q status=%s pay=%s state=%s", uuid, status, pay, state)
	}
	if len(client.requests) != 1 || client.requests[0].PaymentOnDelivery != 0 || len(client.requests[0].Items) != 1 || client.requests[0].Items[0].Price != 1490 {
		t.Fatalf("incorrect CDEK payload: %+v", client.requests)
	}
	worker.sync(ctx)
	if len(client.requests) != 1 {
		t.Fatal("repeat worker duplicated parcel")
	}
	if _, err := pool.Exec(ctx, `UPDATE orders SET payment_status='paid' WHERE id=$1`, orderID); err != nil {
		t.Fatal(err)
	}
	worker.sync(ctx)
	if len(client.requests) != 1 {
		t.Fatal("payment duplicated checkout parcel")
	}
	// Disabled creation does not prevent cancellation of a registered parcel.
	shop.enabled = false
	if _, err := pool.Exec(ctx, `UPDATE orders SET status='cancelled',payment_status='cancelled' WHERE id=$1`, orderID); err != nil {
		t.Fatal(err)
	}
	worker.sync(ctx)
	if len(client.cancelled) != 1 || client.cancelled[0] != uuid {
		t.Fatalf("cancelled order did not cancel parcel: %v", client.cancelled)
	}
	// Unpaid orders made while off must stay outside the queue when enabled later.
	legacyID := makeOrder(false)
	shop.enabled = true
	worker.sync(ctx)
	if len(client.requests) != 1 {
		t.Fatalf("enabling backfilled unpaid order %d", legacyID)
	}
	pendingFeeID := makeOrder(true)
	worker.sync(ctx)
	if len(client.requests) != 1 {
		t.Fatalf("created parcel with unconfirmed packing/fee: %d", pendingFeeID)
	}
	// Lost POST response is resolved with GET, including if cancellation won a race.
	client.ambiguous = true
	unknownID := makeOrder(false)
	worker.sync(ctx)
	if len(client.requests) != 2 {
		t.Fatalf("expected ambiguous create, got %d", len(client.requests))
	}
	if _, err := pool.Exec(ctx, `UPDATE orders SET status='cancelled',payment_status='cancelled',cdek_next_attempt_at=CURRENT_TIMESTAMP WHERE id=$1`, unknownID); err != nil {
		t.Fatal(err)
	}
	worker.sync(ctx)
	if len(client.requests) != 2 || len(client.cancelled) != 2 {
		t.Fatalf("unknown create duplicated or escaped cancellation: creates=%d cancels=%d", len(client.requests), len(client.cancelled))
	}
	// A crashed worker's creation lease must never grant another POST.
	client.ambiguous = false
	interruptedID := makeOrder(false)
	if _, err := pool.Exec(ctx, `UPDATE orders SET cdek_create_state='creating',cdek_next_attempt_at=CURRENT_TIMESTAMP-INTERVAL '1 second' WHERE id=$1`, interruptedID); err != nil {
		t.Fatal(err)
	}
	worker.sync(ctx)
	if len(client.requests) != 2 {
		t.Fatal("expired lease caused a duplicate-risk POST")
	}
	if err := pool.QueryRow(ctx, `SELECT cdek_create_state FROM orders WHERE id=$1`, interruptedID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "unknown" {
		t.Fatalf("interrupted create should require lookup, got %s", state)
	}
	// Two application instances must claim a newly committed checkout only once.
	concurrentID := makeOrder(false)
	secondWorker := NewShippingWorker(pool, client, shop, nil, quietLogger())
	var workers sync.WaitGroup
	workers.Add(2)
	go func() { defer workers.Done(); worker.sync(ctx) }()
	go func() { defer workers.Done(); secondWorker.sync(ctx) }()
	workers.Wait()
	if len(client.requests) != 3 {
		t.Fatalf("concurrent claims duplicated checkout %d: creates=%d", concurrentID, len(client.requests))
	}
	// A real checkout with unavailable stock becomes a preorder and waits.
	if _, err := pool.Exec(ctx, `UPDATE inventory SET available_qty=0 WHERE variant_id=$1`, variantID); err != nil {
		t.Fatal(err)
	}
	preorderID := makeOrder(false)
	worker.sync(ctx)
	if len(client.requests) != 3 {
		t.Fatalf("preorder unexpectedly registered: %d", preorderID)
	}
	// The callback never runs for a failed checkout transaction.
	_, err = service.Create(ctx, CreateInput{Consent: false})
	if err == nil {
		t.Fatal("expected checkout validation error")
	}
	if wakes != 6 {
		t.Fatalf("unexpected checkout wake count: %d", wakes)
	}
	t.Run("seasonal packing quotes and immutable checkout snapshot", func(t *testing.T) {
		// This pool owns one connection. Shadow settings only on that connection;
		// other live test packages never see these season changes.
		if _, err := pool.Exec(ctx, `CREATE TEMP TABLE settings AS TABLE public.settings`); err != nil {
			t.Fatal(err)
		}
		defer func() { _, _ = pool.Exec(ctx, `DROP TABLE pg_temp.settings`) }()
		setSeason := func(season string) {
			t.Helper()
			if _, err := pool.Exec(ctx, `UPDATE settings SET value=$1 WHERE key='cdek.packaging_season'`, season); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := pool.Exec(ctx, `UPDATE inventory SET available_qty=10 WHERE variant_id=$1`, variantID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO variant_attribute_values(variant_id,attribute_id,value) SELECT $1,d.id,to_jsonb(v.n) FROM attribute_definitions d JOIN (VALUES('package_length_cm',40),('package_width_cm',15),('package_height_cm',15),('package_winter_length_cm',40),('package_winter_width_cm',20),('package_winter_height_cm',20)) v(code,n) ON v.code=d.code ON CONFLICT(variant_id,attribute_id) DO UPDATE SET value=EXCLUDED.value`, variantID); err != nil {
			t.Fatal(err)
		}
		checkParcel := func(want integration.Parcel) {
			t.Helper()
			rows, err := loadCheckoutItems(ctx, pool, []ItemInput{{ID: sku, Quantity: 1}}, 0)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 || rows[0].Parcel != want {
				t.Fatalf("checkout packing: %+v, want %+v", rows, want)
			}
			sizes, err := catalog.NewPostgresRepository(pool).PackageSizes(ctx, []string{sku})
			if err != nil {
				t.Fatal(err)
			}
			size := sizes[sku]
			if size.PriceMinor != rows[0].PriceMinor {
				t.Fatalf("quote value differs from shipment value: %d vs %d", size.PriceMinor, rows[0].PriceMinor)
			}
			if size.LengthCM != want.LengthCM || size.WidthCM != want.WidthCM || size.HeightCM != want.HeightCM || size.WeightGrams != want.WeightGrams {
				t.Fatalf("public quote differs: %+v, want %+v", size, want)
			}
		}
		summer := integration.Parcel{LengthCM: 40, WidthCM: 15, HeightCM: 15, WeightGrams: 1500}
		winter := integration.Parcel{LengthCM: 40, WidthCM: 20, HeightCM: 20, WeightGrams: 1500}
		setSeason("summer")
		checkParcel(summer)
		summerID := makeOrder(false)
		setSeason("winter")
		checkParcel(winter)
		winterID := makeOrder(false)
		setSeason("summer")
		for id, want := range map[int64]integration.Parcel{summerID: summer, winterID: winter} {
			_, got, err := worker.shipmentContents(ctx, id)
			if err != nil || got != want {
				t.Fatalf("season switch changed order packing: got %+v, err %v, want %+v", got, err, want)
			}
		}
		// A flip while the carrier is replying invalidates its quote at checkout.
		client.beforeQuote = func(context.Context) { setSeason("winter") }
		changedID := makeOrder(false)
		client.beforeQuote = nil
		var pending int
		var fee float64
		if err := pool.QueryRow(ctx, `SELECT delivery_fee_pending,delivery_fee FROM orders WHERE id=$1`, changedID).Scan(&pending, &fee); err != nil {
			t.Fatal(err)
		}
		if pending != 1 || fee != 0 {
			t.Fatalf("season changed during quote: pending=%d, fee=%v", pending, fee)
		}
		// An incomplete winter set must not mix a summer side into the box.
		if _, err := pool.Exec(ctx, `DELETE FROM variant_attribute_values WHERE variant_id=$1 AND attribute_id=(SELECT id FROM attribute_definitions WHERE code='package_winter_width_cm')`, variantID); err != nil {
			t.Fatal(err)
		}
		missingID := makeOrder(false)
		if err := pool.QueryRow(ctx, `SELECT delivery_fee_pending,delivery_fee FROM orders WHERE id=$1`, missingID).Scan(&pending, &fee); err != nil {
			t.Fatal(err)
		}
		if pending != 1 || fee != 0 {
			t.Fatalf("incomplete winter set quoted: pending=%d, fee=%v", pending, fee)
		}
		// The saved winter order still has its complete packing after catalogue edits.
		_, got, err := worker.shipmentContents(ctx, winterID)
		if err != nil || got != winter {
			t.Fatalf("catalogue edit changed saved packing: %+v, %v", got, err)
		}
		setSeason("summer")
		checkParcel(summer)
	})

}
