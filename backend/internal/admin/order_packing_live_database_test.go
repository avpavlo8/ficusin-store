package admin

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/avpavlo8/ficusin-store/backend/internal/order"
	"github.com/jackc/pgx/v5/pgxpool"
)

type packingQuoteStub struct{ price int }

func (*packingQuoteStub) Configured() bool { return true }
func (stub *packingQuoteStub) CalculatePVZPackages(_ context.Context, _ int, boxes []integration.Parcel) ([]integration.CDEKQuote, error) {
	if len(boxes) != 1 || boxes[0].LengthCM != 42 {
		return nil, fmt.Errorf("wrong measured boxes: %+v", boxes)
	}
	return []integration.CDEKQuote{{TariffCode: 136, TariffName: "До пункта", Price: stub.price}}, nil
}
func (*packingQuoteStub) GetOffices(context.Context, int) ([]integration.CDEKOffice, error) {
	return []integration.CDEKOffice{{Code: "PACKING-PVZ", Location: integration.CDEKLocation{City: "Тест", AddressFull: "Проверенный адрес"}}}, nil
}

type packingSettingsStub struct{}

func (packingSettingsStub) Enabled(string) bool { return true }

func TestOrderPackingFinalizeOnLiveDatabase(t *testing.T) {
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
	email := "packing-" + unique + "@example.invalid"
	var customerID, productID, variantID, warehouseID int64
	var sku string
	if err = pool.QueryRow(ctx, `INSERT INTO customers(email,phone,full_name,consent_at) VALUES($1,$2,'Packing test',CURRENT_TIMESTAMP) RETURNING id`, email, "packing-"+unique).Scan(&customerID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO products(name,slug,status,saby_fields) VALUES('Packing plant',$1,'published',ARRAY['stock']::text[]) RETURNING id`, "packing-"+unique).Scan(&productID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,name,balance) VALUES($1,'Packing plant',10)`, "packing-"+unique); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,saby_id,label,base_price_minor) VALUES($1,$2,'D12',99900) RETURNING id,sku`, productID, "packing-"+unique).Scan(&variantID, &sku); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO warehouses(saby_id,name,city,address,is_active) VALUES('saby-ryazan-main','Основной','Рязань','CI',1) ON CONFLICT(saby_id) DO UPDATE SET is_active=1 RETURNING id`).Scan(&warehouseID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO inventory(warehouse_id,variant_id,available_qty,reserved_qty,synced_at) VALUES($1,$2,10,0,CURRENT_TIMESTAMP)`, warehouseID, variantID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM outbox WHERE recipient=$1`, email)
		_, _ = pool.Exec(ctx, `DELETE FROM payments WHERE order_id IN(SELECT id FROM orders WHERE email=$1)`, email)
		_, _ = pool.Exec(ctx, `DELETE FROM orders WHERE email=$1`, email)
		_, _ = pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, productID)
		_, _ = pool.Exec(ctx, `DELETE FROM saby_nomenclature WHERE saby_id=$1`, "packing-"+unique)
		_, _ = pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, customerID)
	}()
	quote := &packingQuoteStub{price: 840}
	repository := NewPostgresRepository(pool).WithShipmentQuotes(quote).WithOrderPacking(packingSettingsStub{}, true, true, nil)
	actor := Actor{CustomerID: customerID, Role: RoleManager}
	create := func(suffix string) (int64, OrderPackingInput) {
		t.Helper()
		var orderID, itemID int64
		if err = pool.QueryRow(ctx, `INSERT INTO orders(order_number,customer_id,customer_name,phone,email,address,delivery_method,delivery_payee,delivery_fee,delivery_fee_pending,subtotal,total,payment_method,payment_status,status,cdek_city_code,cdek_office_code) VALUES($1,$2,'Packing customer','+70000000000',$3,'Original','cdek','shop',840,1,690,1530,'online','pending','new',44,'PACKING-PVZ') RETURNING id`, "PACK-"+unique+suffix, customerID, email).Scan(&orderID); err != nil {
			t.Fatal(err)
		}
		if err = pool.QueryRow(ctx, `INSERT INTO order_items(order_id,product_id,variant_id,sku,product_name,unit_price,quantity) VALUES($1,$2,$3,$4,'Packing plant',690,1) RETURNING id`, orderID, productID, variantID, sku).Scan(&itemID); err != nil {
			t.Fatal(err)
		}
		fee := 840.0
		return orderID, OrderPackingInput{Revision: 1, TariffCode: 136, QuotedDeliveryFee: &fee, IdempotencyKey: "prepare-" + unique + suffix, Boxes: []ShipmentBoxInput{{LengthCM: 42, WidthCM: 25, HeightCM: 35, WeightGrams: 2000, Contents: []ShipmentBoxContent{{OrderItemID: itemID, Quantity: 1}}}}}
	}
	t.Run("quote and confirmation do not depend on mail", func(t *testing.T) {
		id, input := create("no-mail")
		defer func() { _, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id=$1`, id) }()
		if _, err := pool.Exec(ctx, `UPDATE orders SET email='',cdek_tariff_code=NULL,cdek_last_error='не хватает пункта выдачи или тарифа' WHERE id=$1`, id); err != nil {
			t.Fatal(err)
		}
		repo := NewPostgresRepository(pool).WithShipmentQuotes(quote).WithOrderPacking(packingSettingsStub{}, false, true, nil)
		state, err := repo.OrderPacking(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if len(state.Blockers) != 0 || len(state.QuoteBlockers) != 0 || state.Error != "" || state.Notification.State != "unavailable" {
			t.Fatalf("mail or historical error blocks preparation: %+v", state)
		}
		if _, err = repo.QuoteOrderPacking(ctx, actor, id, input); err != nil {
			t.Fatal(err)
		}
		if _, err = repo.FinalizeOrderPacking(ctx, actor, id, input); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("payment configuration gates confirmation but not quotation", func(t *testing.T) {
		id, input := create("no-payment")
		repo := NewPostgresRepository(pool).WithShipmentQuotes(quote).WithOrderPacking(packingSettingsStub{}, false, false, nil)
		if _, err := repo.QuoteOrderPacking(ctx, actor, id, input); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.FinalizeOrderPacking(ctx, actor, id, input); err == nil {
			t.Fatal("confirmation bypassed payment configuration")
		}
	})
	t.Run("discount is exact repeatable and survives delivery finalization", func(t *testing.T) {
		id, input := create("discount")
		for range 2 {
			if _, err := repository.EditOrder(ctx, actor, id, OrderEdit{Discount: &OrderDiscount{Type: "percent", Value: 10}}); err != nil {
				t.Fatal(err)
			}
		}
		state, err := repository.OrderAdjustment(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if state.Subtotal != 621 || state.DiscountAmount != 69 || state.Items[0].UnitPrice != 690 {
			t.Fatalf("discount compounded or changed history: %+v", state)
		}
		detail, err := order.NewPostgresRepository(pool).DetailForCustomer(ctx, customerID, state.OrderNumber)
		if err != nil || detail.DiscountAmount != 69 || detail.Subtotal != 621 || detail.Total != 1461 {
			t.Fatalf("customer discount differs from manager total: %+v %v", detail, err)
		}

		if _, err := repository.EditOrder(ctx, actor, id, OrderEdit{Discount: &OrderDiscount{Type: "fixed", Value: 700}}); err == nil {
			t.Fatal("accepted excessive discount")
		}
		if _, err := repository.EditOrder(ctx, actor, id, OrderEdit{Discount: &OrderDiscount{Type: "fixed", Value: 0}}); err != nil {
			t.Fatal(err)
		}
		state, err = repository.OrderAdjustment(ctx, id)
		if err != nil || state.Subtotal != 690 {
			t.Fatalf("failed to remove discount: %+v %v", state, err)
		}
		if _, err := repository.EditOrder(ctx, actor, id, OrderEdit{Discount: &OrderDiscount{Type: "fixed", Value: 100}}); err != nil {
			t.Fatal(err)
		}
		packing, err := repository.OrderPacking(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		input.Revision = packing.Revision
		if _, err := repository.FinalizeOrderPacking(ctx, actor, id, input); err != nil {
			t.Fatal(err)
		}
		var total float64
		if err := pool.QueryRow(ctx, `SELECT total::double precision FROM orders WHERE id=$1`, id).Scan(&total); err != nil || total != 1430 {
			t.Fatalf("delivery lost discount: %.2f %v", total, err)
		}
	})
	t.Run("same amount reuses pending payment and concurrent retry", func(t *testing.T) {
		orderID, input := create("same")
		var paymentID int64
		if err = pool.QueryRow(ctx, `INSERT INTO payments(order_id,idempotence_key,amount,status,confirmation_url) VALUES($1,$2,1530,'pending','https://pay.example.test/existing') RETURNING id`, orderID, input.IdempotencyKey).Scan(&paymentID); err != nil {
			t.Fatal(err)
		}
		if _, err = repository.QuoteOrderPacking(ctx, actor, orderID, input); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		failures := make(chan error, 3)
		for range 3 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := repository.FinalizeOrderPacking(ctx, actor, orderID, input)
				failures <- err
			}()
		}
		wg.Wait()
		close(failures)
		for err := range failures {
			if err != nil {
				t.Fatal(err)
			}
		}
		var state string
		var total, price float64
		var count int
		if err = pool.QueryRow(ctx, `SELECT p.status,o.total::double precision,(SELECT unit_price::double precision FROM order_items WHERE order_id=o.id),(SELECT count(*) FROM order_packing_workflows WHERE order_id=o.id) FROM orders o JOIN payments p ON p.order_id=o.id WHERE p.id=$1`, paymentID).Scan(&state, &total, &price, &count); err != nil {
			t.Fatal(err)
		}
		if state != "pending" || total != 1530 || price != 690 || count != 1 {
			t.Fatalf("unexpected payment/order mutation %s %.2f %.2f %d", state, total, price, count)
		}
		var orderStatus string
		if err = pool.QueryRow(ctx, `SELECT status FROM orders WHERE id=$1`, orderID).Scan(&orderStatus); err != nil || orderStatus != "confirmed" {
			t.Fatalf("packing must confirm the order: %s %v", orderStatus, err)
		}
		if _, err = repository.EditOrder(ctx, actor, orderID, OrderEdit{}); err == nil {
			t.Fatal("accepted contents edit after packing was frozen")
		}
		changed := input
		changed.Boxes = append([]ShipmentBoxInput(nil), input.Boxes...)
		changed.Boxes[0].HeightCM++
		if _, err = repository.FinalizeOrderPacking(ctx, actor, orderID, changed); err == nil {
			t.Fatal("reused operation key with changed payload")
		}
	})
	t.Run("changed tariff rejected before any write", func(t *testing.T) {
		orderID, input := create("price")
		quote.price = 900
		defer func() { quote.price = 840 }()
		if _, err = repository.FinalizeOrderPacking(ctx, actor, orderID, input); err == nil || !strings.Contains(err.Error(), "изменилась") {
			t.Fatalf("expected stale quote error: %v", err)
		}
		var fee float64
		var count int
		if err = pool.QueryRow(ctx, `SELECT delivery_fee::double precision,(SELECT count(*) FROM order_packing_workflows WHERE order_id=orders.id) FROM orders WHERE id=$1`, orderID).Scan(&fee, &count); err != nil {
			t.Fatal(err)
		}
		if fee != 840 || count != 0 {
			t.Fatal("stale quote mutated order")
		}
		approved := 900.0
		input.QuotedDeliveryFee = &approved
		if _, err = pool.Exec(ctx, `INSERT INTO payments(order_id,idempotence_key,amount,status,confirmation_url) VALUES($1,$2,1530,'pending','https://pay.example.test/old')`, orderID, input.IdempotencyKey); err != nil {
			t.Fatal(err)
		}
		if _, err = repository.FinalizeOrderPacking(ctx, actor, orderID, input); err != nil {
			t.Fatal(err)
		}
		var status string
		if err = pool.QueryRow(ctx, `SELECT status FROM payments WHERE order_id=$1`, orderID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != "superseded" {
			t.Fatalf("old amount link not retired: %s", status)
		}
	})
	t.Run("destination verification and partial offers", func(t *testing.T) {
		orderID, input := create("pvz")
		input.CityCode = 44
		input.OfficeCode = "WRONG"
		if _, err = repository.QuoteOrderPacking(ctx, actor, orderID, input); err == nil {
			t.Fatal("invalid office accepted")
		}
		input.OfficeCode = "PACKING-PVZ"
		var offerID int64
		if err = pool.QueryRow(ctx, `INSERT INTO shipment_offers(order_id,public_token,order_revision,status,delivery_method) VALUES($1,$2,1,'offered','cdek') RETURNING id`, orderID, input.IdempotencyKey).Scan(&offerID); err != nil {
			t.Fatal(err)
		}
		if _, err = repository.FinalizeOrderPacking(ctx, actor, orderID, input); err == nil {
			t.Fatal("active offered shipment accepted")
		}
		if _, err = pool.Exec(ctx, `UPDATE shipment_offers SET status='draft' WHERE id=$1`, offerID); err != nil {
			t.Fatal(err)
		}
		if _, err = repository.FinalizeOrderPacking(ctx, actor, orderID, input); err != nil {
			t.Fatal(err)
		}
		var status, address string
		if err = pool.QueryRow(ctx, `SELECT so.status,o.address FROM shipment_offers so JOIN orders o ON o.id=so.order_id WHERE so.id=$1`, offerID).Scan(&status, &address); err != nil {
			t.Fatal(err)
		}
		if status != "stale" || address != "Проверенный адрес" {
			t.Fatalf("unsafe draft/destination result %s %s", status, address)
		}
	})
	t.Run("paid plants acquire an unpaid delivery balance", func(t *testing.T) {
		orderID, input := create("paid-plants")
		if _, err = pool.Exec(ctx, `UPDATE orders SET delivery_fee=0,total=subtotal,payment_status='paid' WHERE id=$1`, orderID); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO payments(order_id,idempotence_key,amount,status) VALUES($1,$2,690,'paid')`, orderID, input.IdempotencyKey); err != nil {
			t.Fatal(err)
		}
		if _, err = repository.FinalizeOrderPacking(ctx, actor, orderID, input); err != nil {
			t.Fatal(err)
		}
		var status string
		var total float64
		if err = pool.QueryRow(ctx, `SELECT payment_status,total::double precision FROM orders WHERE id=$1`, orderID).Scan(&status, &total); err != nil {
			t.Fatal(err)
		}
		if status != "partially_paid" || total != 1530 {
			t.Fatalf("delivery top-up still marked paid: %s %.2f", status, total)
		}
	})
	t.Run("legacy provider link blocks changed total", func(t *testing.T) {
		orderID, input := create("legacy")
		quote.price = 900
		defer func() { quote.price = 840 }()
		approved := 900.0
		input.QuotedDeliveryFee = &approved
		if _, err = pool.Exec(ctx, `INSERT INTO payments(order_id,idempotence_key,amount,status,provider_payment_id,confirmation_url) VALUES($1,$2,1530,'pending',$3,'https://pay.example.test/legacy')`, orderID, input.IdempotencyKey, "legacy-"+unique); err != nil {
			t.Fatal(err)
		}
		if _, err = repository.FinalizeOrderPacking(ctx, actor, orderID, input); err == nil || !strings.Contains(err.Error(), "старого типа") {
			t.Fatalf("legacy live link was superseded unsafely: %v", err)
		}
		var status string
		if err = pool.QueryRow(ctx, `SELECT status FROM payments WHERE order_id=$1`, orderID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != "pending" {
			t.Fatal("rejected finalization mutated payment")
		}
	})
}
