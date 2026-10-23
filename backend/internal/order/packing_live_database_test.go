package order

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/avpavlo8/ficusin-store/backend/internal/mail"
	"github.com/avpavlo8/ficusin-store/backend/internal/payment"
	"github.com/jackc/pgx/v5/pgxpool"
)

type packingCarrierStub struct {
	*stage13CDEK
	requests []integration.ShipmentRequest
}

func (client *packingCarrierStub) CreateOrder(_ context.Context, request integration.ShipmentRequest) (integration.Shipment, error) {
	client.requests = append(client.requests, request)
	client.creates++
	shipment := integration.Shipment{UUID: "packing-" + request.OrderNumber, TrackNumber: "TRACK-" + request.OrderNumber, Status: "CREATED"}
	client.byNumber[request.OrderNumber] = shipment
	client.byUUID[shipment.UUID] = shipment
	if client.ambiguous {
		return integration.Shipment{}, fmt.Errorf("%w: response lost", integration.ErrCDEKOutcomeUnknown)
	}
	return shipment, nil
}

type packingPaymentStub struct {
	fail  bool
	calls int
}

type packingExpirySettings struct{}

func (packingExpirySettings) Number(string) int { return 24 }

func (*packingPaymentStub) Configured() bool { return true }
func (*packingPaymentStub) BalanceForOrder(context.Context, int64) (payment.Balance, error) {
	return payment.Balance{Total: 1530, Due: 1530, Ready: true}, nil
}
func (stub *packingPaymentStub) StartOutstandingForOrderID(context.Context, int64) (string, payment.Balance, error) {
	stub.calls++
	if stub.fail {
		return "", payment.Balance{}, errors.New("провайдер не ответил; существующая попытка проверяется")
	}
	return "https://pay.example.test/packing", payment.Balance{Due: 840}, nil
}

type packingSenderStub struct {
	before func(mail.Letter)
	fail   bool
	sent   []mail.Letter
}

func (*packingSenderStub) Configured() bool { return true }
func (stub *packingSenderStub) Send(_ context.Context, letter mail.Letter) error {
	if stub.before != nil {
		stub.before(letter)
	}
	if stub.fail {
		return errors.New("SMTP temporary failure")
	}
	stub.sent = append(stub.sent, letter)
	return nil
}

func TestOrderPackingLifecycleOnLiveDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	unique := fmt.Sprint(time.Now().UnixNano())
	email := "packing-worker-" + unique + "@example.invalid"
	number := "PACK-WORK-" + unique
	var productID, variantID, warehouseID, orderID, itemID, packingID int64
	var sku string
	if err = pool.QueryRow(ctx, `INSERT INTO products(name,slug,status,saby_fields) VALUES('Measured plant',$1,'published',ARRAY['stock']::text[]) RETURNING id`, "packing-worker-"+unique).Scan(&productID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,name,balance) VALUES($1,'Measured plant',10)`, "packing-worker-"+unique); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,saby_id,label,base_price_minor) VALUES($1,$2,'D12',99900) RETURNING id,sku`, productID, "packing-worker-"+unique).Scan(&variantID, &sku); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO warehouses(saby_id,name,city,address,is_active) VALUES('saby-ryazan-main','Основной','Рязань','CI',1) ON CONFLICT(saby_id) DO UPDATE SET is_active=1 RETURNING id`).Scan(&warehouseID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO inventory(warehouse_id,variant_id,available_qty,reserved_qty,synced_at) VALUES($1,$2,10,0,CURRENT_TIMESTAMP)`, warehouseID, variantID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO orders(order_number,customer_name,phone,email,address,delivery_method,delivery_payee,delivery_fee,subtotal,total,payment_method,payment_status,status,cdek_city_code,cdek_office_code,cdek_tariff_code,cdek_checkout_requested,created_at) VALUES($1,'Packing worker','+70000000000',$2,'Test office','cdek','shop',840,690,1530,'online','pending','new',44,'PACKING-PVZ',136,1,CURRENT_TIMESTAMP-INTERVAL '30 days') RETURNING id`, number, email).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO order_items(order_id,product_id,variant_id,sku,product_name,unit_price,quantity) VALUES($1,$2,$3,$4,'Measured plant',690,1) RETURNING id`, orderID, productID, variantID, sku).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	boxes := fmt.Sprintf(`[{"lengthCm":42,"widthCm":25,"heightCm":35,"weightGrams":2000,"contents":[{"orderItemId":%d,"quantity":1}]}]`, itemID)
	if err = pool.QueryRow(ctx, `INSERT INTO order_packing_workflows(order_id,order_revision,idempotency_key,fingerprint,boxes,tariff_code,delivery_fee) VALUES($1,1,$2,'fixture',$3,136,840) RETURNING id`, orderID, unique, boxes).Scan(&packingID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM outbox WHERE recipient=$1`, email)
		_, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id=$1`, orderID)
		_, _ = pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, productID)
		_, _ = pool.Exec(ctx, `DELETE FROM saby_nomenclature WHERE saby_id=$1`, "packing-worker-"+unique)
	}()
	// Replacement applications use a distinct provider number. Ambiguous
	// creation must reconcile this revision, never the previously removed one.
	if _, err = pool.Exec(ctx, `UPDATE orders SET cdek_order_number=order_number||'-r2' WHERE id=$1`, orderID); err != nil {
		t.Fatal(err)
	}
	carrier := &packingCarrierStub{stage13CDEK: &stage13CDEK{ambiguous: true, byNumber: map[string]integration.Shipment{}, byUUID: map[string]integration.Shipment{}}}
	shipping := NewShippingWorker(pool, carrier, stage13ShippingSettings{}, nil, quietLogger())
	payments := &packingPaymentStub{fail: true}
	preparation := NewPackingWorker(pool, payments, "https://ficusin.example.test", quietLogger())
	runPreparation := func() {
		t.Helper()
		if _, err = pool.Exec(ctx, `UPDATE order_packing_workflows SET next_attempt_at=NULL WHERE id=$1`, packingID); err != nil {
			t.Fatal(err)
		}
		preparation.process(ctx)
	}
	countLetters := func() int {
		t.Helper()
		var count int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE order_packing_id=$1`, packingID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	expiry := NewExpiryWorker(pool, packingExpirySettings{}, nil, quietLogger())
	expiry.process(ctx)
	var notExpired string
	if err = pool.QueryRow(ctx, `SELECT status FROM orders WHERE id=$1`, orderID).Scan(&notExpired); err != nil {
		t.Fatal(err)
	}
	if notExpired != "new" {
		t.Fatal("preparation expired before payment notification")
	}
	if err = expiry.cancel(ctx, orderID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT status FROM orders WHERE id=$1`, orderID).Scan(&notExpired); err != nil {
		t.Fatal(err)
	}
	if notExpired != "new" {
		t.Fatal("stale expiry candidate cancelled a preparing order")
	}

	runPreparation()
	if payments.calls != 0 || countLetters() != 0 {
		t.Fatal("payment or notification started before courier registration")
	}
	shipping.sync(ctx)
	if carrier.creates != 1 || carrier.requests[0].OrderNumber != number+"-r2" || len(carrier.requests[0].Packages) != 1 || carrier.requests[0].Packages[0].Box.LengthCM != 42 || carrier.requests[0].Packages[0].Items[0].Price != 690 {
		t.Fatalf("wrong carrier request: %+v", carrier.requests)
	}
	runPreparation()
	if payments.calls != 0 || countLetters() != 0 {
		t.Fatal("ambiguous courier result was treated as registered")
	}
	if _, err = pool.Exec(ctx, `UPDATE orders SET cdek_next_attempt_at=CURRENT_TIMESTAMP WHERE id=$1`, orderID); err != nil {
		t.Fatal(err)
	}
	shipping.sync(ctx)
	if carrier.creates != 1 {
		t.Fatal("ambiguous creation retried POST instead of GET")
	}
	runPreparation()
	if payments.calls != 1 || countLetters() != 0 {
		t.Fatal("payment failure produced customer notification")
	}
	payments.fail = false
	runPreparation()
	if countLetters() != 1 {
		t.Fatal("ready workflow did not enqueue one notification")
	}
	runPreparation()
	if payments.calls != 2 || countLetters() != 1 {
		t.Fatal("completed preparation was repeated")
	}
	var body string
	if err = pool.QueryRow(ctx, `SELECT body FROM outbox WHERE order_packing_id=$1`, packingID).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "К оплате: 840.00 ₽") || strings.Contains(body, "К оплате: 1530") || !strings.Contains(body, "https://pay.example.test/packing") || !strings.Contains(body, "https://ficusin.example.test/account/orders/") || !strings.Contains(body, "TRACK-") {
		t.Fatalf("incomplete customer message: %s", body)
	}
	sender := &packingSenderStub{fail: true}
	letters := NewLetterWorker(pool, sender, quietLogger())
	letters.process(ctx)
	var attempts int
	var sentAt *time.Time
	if err = pool.QueryRow(ctx, `SELECT attempts,sent_at FROM outbox WHERE order_packing_id=$1`, packingID).Scan(&attempts, &sentAt); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 || sentAt != nil {
		t.Fatal("SMTP failure claimed delivery")
	}
	sender.fail = false
	letters.process(ctx)
	letters.process(ctx)
	var state string
	if err = pool.QueryRow(ctx, `SELECT state FROM order_packing_workflows WHERE id=$1`, packingID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	sentToCustomer := 0
	for _, letter := range sender.sent {
		if letter.To == email {
			sentToCustomer++
		}
	}
	if state != "notified" || sentToCustomer != 1 {
		t.Fatalf("unexpected final notification state %s, sends to this customer %d", state, sentToCustomer)
	}
	expiry.process(ctx)
	var orderStatus string
	if err = pool.QueryRow(ctx, `SELECT status FROM orders WHERE id=$1`, orderID).Scan(&orderStatus); err != nil {
		t.Fatal(err)
	}
	if orderStatus != "new" {
		t.Fatalf("registration pretended physical shipment: %s", orderStatus)
	}
}

func TestMeasuredOrderPackingRejectsIncompleteContents(t *testing.T) {
	var boxes []measuredOrderBox
	if err := json.Unmarshal([]byte(`[{"lengthCm":42,"widthCm":25,"heightCm":35,"weightGrams":2000,"contents":[{"orderItemId":7,"quantity":1}]}]`), &boxes); err != nil {
		t.Fatal(err)
	}
	items := map[int64]integration.ShipmentItem{7: {Name: "Plant", Price: 690, Quantity: 2}}
	if _, err := measuredPackages("test", boxes, items); err == nil {
		t.Fatal("partially packed order accepted as full")
	}
	items[7] = integration.ShipmentItem{Name: "Plant", Price: 690, Quantity: 1}
	boxes[0].HeightCM = 0
	if _, err := measuredPackages("test", boxes, items); err == nil {
		t.Fatal("zero dimensions converted to real courier request")
	}
}

func TestPackingLetterCancellationAfterClaimOnLiveDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	recipient := fmt.Sprintf("packing-cancel-%d@example.invalid", time.Now().UnixNano())
	var second int64
	if _, err = pool.Exec(ctx, `INSERT INTO outbox(recipient,subject,body) VALUES($1,'first','fixture')`, recipient); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO outbox(recipient,subject,body) VALUES($1,'second','fixture') RETURNING id`, recipient).Scan(&second); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM outbox WHERE recipient=$1`, recipient) }()
	sender := &packingSenderStub{}
	sender.before = func(letter mail.Letter) {
		if letter.To == recipient && letter.Subject == "first" {
			var token string
			if err = pool.QueryRow(ctx, `SELECT lock_token FROM outbox WHERE id=$1`, second).Scan(&token); err != nil || token == "" {
				t.Fatalf("second message was not claimed before cancellation: %s %v", token, err)
			}
			if _, err = pool.Exec(ctx, `UPDATE outbox SET cancelled_at=CURRENT_TIMESTAMP WHERE id=$1`, second); err != nil {
				t.Fatal(err)
			}
		}
	}
	NewLetterWorker(pool, sender, quietLogger()).process(ctx)
	count := 0
	for _, letter := range sender.sent {
		if letter.To == recipient {
			count++
			if letter.Subject == "second" {
				t.Fatal("cancelled queued message was sent")
			}
		}
	}
	if count != 1 {
		t.Fatalf("unexpected notifications %d", count)
	}
}

func TestOrderStatusFollowsTrackingOnLiveDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	number := fmt.Sprintf("TRACK-STATUS-%d", time.Now().UnixNano())
	var id int64
	if err := pool.QueryRow(ctx, `INSERT INTO orders(order_number,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,payment_status,status,cdek_uuid) VALUES($1,'Tracking fixture','+70000000000','tracking@example.invalid','cdek',0,690,690,'paid','assembling',$1) RETURNING id`, number).Scan(&id); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id=$1`, id) }()
	client := &stage13CDEK{byUUID: map[string]integration.Shipment{}, byNumber: map[string]integration.Shipment{}}
	worker := NewShippingWorker(pool, client, stage13ShippingSettings{}, nil, quietLogger())
	for _, tc := range []struct{ carrier, want string }{
		{"CREATED", "assembling"},
		{"RECEIVED_AT_SHIPMENT_WAREHOUSE", "shipped"},
		{"ACCEPTED_AT_PICK_UP_POINT", "shipped"},
		{"NOT_DELIVERED", "shipped"},
		{"DELIVERED", "completed"},
		{"IN_TRANSIT", "completed"},
	} {
		client.byUUID[number] = integration.Shipment{UUID: number, TrackNumber: number, Status: tc.carrier}
		worker.refreshStatuses(ctx)
		var got string
		if err := pool.QueryRow(ctx, `SELECT status FROM orders WHERE id=$1`, id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Fatalf("carrier %s => %s, want %s", tc.carrier, got, tc.want)
		}
	}
}
