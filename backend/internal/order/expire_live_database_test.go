package order

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type expiryHours int

func (hours expiryHours) Number(string) int { return int(hours) }

type expiryPaymentFence struct {
	refused int64
	calls   map[int64]int
}

func (provider *expiryPaymentFence) CancelPending(_ context.Context, id int64) error {
	provider.calls[id]++
	if id == provider.refused {
		return errors.New("payment outcome requires reconciliation")
	}
	return nil
}

func TestExpiryCancelsOnlyEligibleOrdersOnLiveDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	base, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	schema := fmt.Sprintf("expiry_matrix_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = base.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	defer base.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE")
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		_, err := pool.Exec(ctx, sql, args...)
		must(err)
	}
	// Private copies of the migrated tables isolate the worker's global scan.
	// No application workers or external transports are started by this test.
	for _, table := range []string{"orders", "order_items", "product_variants", "inventory", "stock_movements", "payments", "shipment_offers", "outbox", "order_packing_workflows", "order_sale_intents"} {
		id := pgx.Identifier{table}.Sanitize()
		exec("CREATE TABLE " + id + " (LIKE public." + id + " INCLUDING DEFAULTS INCLUDING GENERATED INCLUDING IDENTITY INCLUDING CONSTRAINTS INCLUDING INDEXES)")
	}
	type scenario struct {
		name, method, payment, status, packing string
		preorder, feePending, ageHours         int
		cancel, refuse                         bool
		id, variant                            int64
	}
	scenarios := []scenario{
		{name: "abandoned online", cancel: true},
		{name: "pay at pickup", method: "on_delivery"},
		{name: "invoice", method: "invoice"},
		{name: "preorder", preorder: 1},
		{name: "delivery quote pending", feePending: 1},
		{name: "already paid", payment: "paid"},
		{name: "completed", status: "completed"},
		{name: "cancelled", status: "cancelled"},
		{name: "recent", ageHours: 1},
		{name: "packing not notified", packing: "unnotified"},
		{name: "packing recently notified", packing: "recent"},
		{name: "packing invitation expired", packing: "expired", cancel: true},
		{name: "provider refuses cancellation", refuse: true},
	}
	provider := &expiryPaymentFence{calls: map[int64]int{}}
	for index := range scenarios {
		item := &scenarios[index]
		if item.method == "" {
			item.method = "online"
		}
		if item.payment == "" {
			item.payment = "pending"
		}
		if item.status == "" {
			item.status = "new"
		}
		if item.ageHours == 0 {
			item.ageHours = 48
		}
		must(pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,label,base_price_minor) VALUES(1,$1,10000) RETURNING id`, item.name).Scan(&item.variant))
		must(pool.QueryRow(ctx, `INSERT INTO orders(order_number,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,payment_method,payment_status,status,has_preorder,delivery_fee_pending,created_at) VALUES($1,'Expiry fixture','+70000000000','','pickup',0,100,100,$2,$3,$4,$5,$6,NOW()-make_interval(hours=>$7)) RETURNING id`, item.name, item.method, item.payment, item.status, item.preorder, item.feePending, item.ageHours).Scan(&item.id))
		exec(`INSERT INTO order_items(order_id,product_id,variant_id,sku,product_name,unit_price,quantity,reserved_qty) SELECT $1,1,id,sku,'Expiry fixture',100,2,0 FROM product_variants WHERE id=$2`, item.id, item.variant)
		exec(`INSERT INTO inventory(warehouse_id,variant_id,available_qty,reserved_qty) VALUES(1,$1,10,0)`, item.variant)
		exec(`INSERT INTO payments(order_id,idempotence_key,amount,status) VALUES($1,$2,100,'pending')`, item.id, item.name)
		var offer int64
		must(pool.QueryRow(ctx, `INSERT INTO shipment_offers(order_id,public_token,order_revision,status,delivery_method) VALUES($1,$2,1,'offered','pickup') RETURNING id`, item.id, item.name).Scan(&offer))
		exec(`INSERT INTO outbox(recipient,subject,body,shipment_offer_id) VALUES('expiry@example.invalid','Fixture','Fixture',$1)`, offer)
		if item.packing != "" {
			var notified *time.Time
			if item.packing != "unnotified" {
				hours := 1
				if item.packing == "expired" {
					hours = 48
				}
				must(pool.QueryRow(ctx, `SELECT NOW()-make_interval(hours=>$1)`, hours).Scan(&notified))
			}
			exec(`INSERT INTO order_packing_workflows(order_id,order_revision,idempotency_key,fingerprint,boxes,tariff_code,delivery_fee,state,notified_at) VALUES($1,1,$2,$2,'[]',136,0,'notified',$3)`, item.id, item.name, notified)
		}
		if item.refuse {
			provider.refused = item.id
		}
	}
	NewExpiryWorker(pool, expiryHours(0), provider, quietLogger()).process(ctx)
	if len(provider.calls) != 0 {
		t.Fatal("disabled expiry called the payment provider")
	}
	worker := NewExpiryWorker(pool, expiryHours(24), provider, quietLogger())
	worker.process(ctx)
	// The guarded cancel path independently rejects stale/ineligible candidates.
	for _, item := range scenarios {
		if !item.cancel && !item.refuse {
			must(worker.cancel(ctx, item.id))
		}
	}
	worker.process(ctx)
	for _, item := range scenarios {
		t.Run(item.name, func(t *testing.T) {
			var status, payment, attempt, offer string
			var released, letterCancelled bool
			var available, reserved, releases int
			var amount int64
			if err := pool.QueryRow(ctx, `SELECT o.status,o.payment_status,o.stock_released_at IS NOT NULL,p.status,ROUND(p.amount*100)::BIGINT,i.available_qty,i.reserved_qty,s.status,b.cancelled_at IS NOT NULL,(SELECT COUNT(*)::INT FROM stock_movements WHERE order_id=o.id AND kind='release') FROM orders o JOIN payments p ON p.order_id=o.id JOIN order_items oi ON oi.order_id=o.id JOIN inventory i ON i.variant_id=oi.variant_id JOIN shipment_offers s ON s.order_id=o.id JOIN outbox b ON b.shipment_offer_id=s.id WHERE o.id=$1`, item.id).Scan(&status, &payment, &released, &attempt, &amount, &available, &reserved, &offer, &letterCancelled, &releases); err != nil {
				t.Fatal(err)
			}
			if amount != 10000 || available != 10 {
				t.Fatalf("expiry rewrote money or physical stock: %d/%d", amount, available)
			}
			if item.cancel {
				if status != "cancelled" || payment != "cancelled" || attempt != "cancelled" || released || reserved != 0 || offer != "cancelled" || !letterCancelled || releases != 0 || provider.calls[item.id] != 1 {
					t.Fatalf("eligible expiry was incomplete or repeated: %s/%s/%s release=%v reserved=%d offer=%s letter=%v journal=%d provider=%d", status, payment, attempt, released, reserved, offer, letterCancelled, releases, provider.calls[item.id])
				}
			} else {
				calls := 0
				if item.refuse {
					calls = 2
				}
				if status != item.status || payment != item.payment || attempt != "pending" || released || reserved != 0 || offer != "offered" || letterCancelled || releases != 0 || provider.calls[item.id] != calls {
					t.Fatalf("protected order was changed: %s/%s/%s release=%v reserved=%d offer=%s letter=%v journal=%d provider=%d", status, payment, attempt, released, reserved, offer, letterCancelled, releases, provider.calls[item.id])
				}
			}
		})
	}
}
