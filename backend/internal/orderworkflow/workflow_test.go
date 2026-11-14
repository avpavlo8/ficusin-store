package orderworkflow_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	flow "github.com/avpavlo8/ficusin-store/backend/internal/orderworkflow"
	"github.com/jackc/pgx/v5/pgxpool"
)

func readyFacts() flow.Facts {
	return flow.Facts{Status: "new", PaymentStatus: "paid", PaymentMethod: "online", DeliveryMethod: "cdek", AddressConfirmed: true, Lines: []flow.Line{{Name: "Нолина", Wanted: 1, Available: 2, StockKnown: true, Dimensions: true}}}
}
func has(s flow.State, code string) bool {
	for _, b := range s.Blockers {
		if b.Code == code {
			return true
		}
	}
	return false
}
func TestAssemblyReadinessUsesPaymentFreshStockAndPacking(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*flow.Facts)
		code   string
	}{
		{"unpaid", func(f *flow.Facts) { f.PaymentStatus = "pending" }, "payment"},
		{"unknown stock", func(f *flow.Facts) { f.Lines[0].StockKnown = false }, "stock_unknown"},
		{"short stock", func(f *flow.Facts) { f.Lines[0].Available = 0 }, "stock_short"},
		{"dimensions", func(f *flow.Facts) { f.Lines[0].Dimensions = false }, "dimensions"},
		{"delivery", func(f *flow.Facts) { f.FeePending = true }, "delivery_fee"},
		{"repack", func(f *flow.Facts) { f.RepackRequested = true }, "repack"},
		{"address", func(f *flow.Facts) { f.AddressConfirmed = false }, "delivery_address"},
		{"unknown payment", func(f *flow.Facts) { f.PaymentUncertain = true }, "payment_check"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := readyFacts()
			tc.mutate(&f)
			s := flow.Evaluate(f)
			if s.ReadyForAssembly || s.Queue != "new" || !has(s, tc.code) {
				t.Fatalf("missing truthful blocker: %+v", s)
			}
		})
	}
	s := flow.Evaluate(readyFacts())
	if !s.ReadyForAssembly || len(s.Blockers) != 0 {
		t.Fatal(s)
	}
	f := readyFacts()
	f.Lines[0].Dimensions = false
	f.BoxesConfirmed = true
	if !flow.Evaluate(f).ReadyForAssembly {
		t.Fatal("measured complete boxes were ignored")
	}
	f.Status = "shipped"
	f.PaymentStatus = "pending"
	f.Lines[0].StockKnown = false
	if s := flow.Evaluate(f); s.Queue != "in_delivery" || len(s.Blockers) != 0 {
		t.Fatal("carrier milestone was rewound", s)
	}
}
func TestPickupCashNeedsExplicitManualAssemblyAndPaymentAtHandover(t *testing.T) {
	f := readyFacts()
	f.DeliveryMethod = "pickup"
	f.PaymentMethod = "on_delivery"
	f.PaymentStatus = "on_delivery"
	s := flow.Evaluate(f)
	if s.ReadyForAssembly || s.Queue != "new" || len(s.ManualStatuses) != 2 || s.ManualStatuses[0] != "assembling" {
		t.Fatal(s)
	}
	if !strings.Contains(s.Blockers[0].Message, "вручную") {
		t.Fatal("cash pickup instruction missing")
	}
	f.Status = "assembling"
	if s = flow.Evaluate(f); s.Queue != "in_progress" || s.ManualStatuses[0] != "ready" {
		t.Fatal(s)
	}
	f.Status = "ready"
	if s = flow.Evaluate(f); s.ManualStatuses[0] != "completed" {
		t.Fatal(s)
	}
	f.Status = "new"
	f.PaymentMethod = "online"
	if s = flow.Evaluate(f); len(s.ManualStatuses) != 0 {
		t.Fatal("unpaid online order can be picked", s)
	}
}

func TestItemBlockersIdentifyExactOrderRowsRatherThanNames(t *testing.T) {
	f := readyFacts()
	f.PaymentStatus = "pending"
	f.FeePending = true
	f.Lines = []flow.Line{
		{Name: "Нолина", OrderItemIDs: []int64{11, 13}, Wanted: 2, StockKnown: false, Dimensions: true},
		{Name: "Нолина", OrderItemIDs: []int64{12}, Wanted: 2, Available: 1, StockKnown: true, Dimensions: true},
		{Name: "Нолина", OrderItemIDs: []int64{14}, Wanted: 1, Available: 1, StockKnown: true, Dimensions: false},
	}
	want := map[string][]int64{"stock_unknown": {11, 13}, "stock_short": {12}, "dimensions": {14}, "payment": nil, "delivery_fee": nil}
	s := flow.Evaluate(f)
	if len(s.Blockers) != len(want) {
		t.Fatalf("unexpected blockers: %+v", s.Blockers)
	}
	for _, blocker := range s.Blockers {
		ids, ok := want[blocker.Code]
		if !ok || !slices.Equal(blocker.OrderItemIDs, ids) {
			t.Fatalf("%s: order rows %v, want %v", blocker.Code, blocker.OrderItemIDs, ids)
		}
		raw, err := json.Marshal(blocker)
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]json.RawMessage
		if err = json.Unmarshal(raw, &payload); err != nil {
			t.Fatal(err)
		}
		if _, present := payload["orderItemIds"]; present != (len(ids) > 0) {
			t.Fatalf("incorrect orderItemIds omission: %s", raw)
		}
	}
}

func TestOrderWorkflowLocalDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var db string
	if err = pool.QueryRow(ctx, "SELECT current_database()").Scan(&db); err != nil {
		t.Fatal(err)
	}
	if db != "ficusin_local_publisher_test" {
		t.Skip("requires isolated local publisher test database")
	}
	tag := fmt.Sprint(time.Now().UnixNano())
	sabyID := "workflow-" + tag
	var product, variant, warehouse, order, customer, orderItem int64
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at) VALUES($1,$2,'','Workflow test',NOW()) RETURNING id`, tag+"@example.invalid", "+7"+tag[len(tag)-10:]).Scan(&customer))
	must(pool.QueryRow(ctx, `INSERT INTO products(name,slug,status,saby_fields) VALUES('Workflow plant',$1,'published',ARRAY['stock']) RETURNING id`, sabyID).Scan(&product))
	defer func() {
		pool.Exec(ctx, `DELETE FROM stock_movements WHERE order_id=$1`, order)
		pool.Exec(ctx, `DELETE FROM admin_audit_log WHERE actor_customer_id=$1`, customer)
		pool.Exec(ctx, `DELETE FROM orders WHERE id=$1`, order)
		pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, product)
		pool.Exec(ctx, `DELETE FROM saby_nomenclature WHERE saby_id=$1`, sabyID)
		pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, customer)
	}()
	_, err = pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,name) VALUES($1,'Workflow plant')`, sabyID)
	must(err)
	must(pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,label,base_price_minor,is_active,saby_id) VALUES($1,'D13',10000,1,$2) RETURNING id`, product, sabyID).Scan(&variant))
	_, err = pool.Exec(ctx, `INSERT INTO warehouses(saby_id,name,city,address,is_active) VALUES('saby-ryazan-main','Main','Рязань','test',1) ON CONFLICT(saby_id) DO NOTHING`)
	must(err)
	must(pool.QueryRow(ctx, `SELECT id FROM warehouses WHERE saby_id='saby-ryazan-main'`).Scan(&warehouse))
	_, err = pool.Exec(ctx, `INSERT INTO inventory(warehouse_id,variant_id,available_qty,reserved_qty,synced_at) VALUES($1,$2,3,0,NOW())`, warehouse, variant)
	must(err)
	must(pool.QueryRow(ctx, `INSERT INTO orders(order_number,customer_id,customer_name,phone,email,address,delivery_method,delivery_fee,subtotal,total,payment_method,payment_status,status,has_preorder,delivery_fee_pending,cdek_tariff_code,cdek_office_code,cdek_status) VALUES($1,$2,'Workflow','+70000000000','','test','cdek',100,100,200,'online','paid','new',1,0,136,'TEST','CREATED') RETURNING id`, "WORKFLOW-"+tag, customer).Scan(&order))
	must(pool.QueryRow(ctx, `INSERT INTO order_items(order_id,product_id,variant_id,sku,product_name,variant_label,variant_snapshot,unit_price,quantity) SELECT $1,$2,id,sku,'Workflow plant','D13','{"packageLengthCm":30,"packageWidthCm":20,"packageHeightCm":40,"packageWeightGrams":1000}',100,1 FROM product_variants WHERE id=$3 RETURNING id`, order, product, variant).Scan(&orderItem))
	must(flow.Refresh(ctx, pool, []int64{order}))
	states, err := flow.Read(ctx, pool, []int64{order})
	must(err)
	if s := states[order]; s.Status != "assembling" || s.Queue != "in_progress" || !s.StockConfirmed {
		t.Fatal("paid fresh stock did not advance; CREATED must not mean shipped", s)
	}
	repo := admin.NewPostgresRepository(pool)
	actor := admin.Actor{CustomerID: customer, Role: admin.RoleOwner}
	if _, err = repo.UpdateOrderStatus(ctx, actor, order, "shipped", ""); err == nil {
		t.Fatal("manual CDEK shipping accepted")
	}
	for _, tc := range []struct{ name, setup, code string }{
		{"unpaid", `UPDATE orders SET status='new',payment_status='pending' WHERE id=$1`, "payment"},
		{"stale stock", `UPDATE inventory SET synced_at=NOW()-INTERVAL '3 hours' WHERE variant_id=$1`, "stock_unknown"},
		{"no stock", `UPDATE inventory SET available_qty=0 WHERE variant_id=$1`, "stock_short"},
		{"missing dimensions", `UPDATE order_items SET variant_snapshot='{}' WHERE order_id=$1`, "dimensions"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, e := pool.Exec(ctx, `UPDATE orders SET status='new',payment_status='paid' WHERE id=$1`, order)
			must(e)
			_, e = pool.Exec(ctx, `UPDATE inventory SET available_qty=3,synced_at=NOW() WHERE variant_id=$1`, variant)
			must(e)
			_, e = pool.Exec(ctx, `UPDATE order_items SET variant_snapshot='{"packageLengthCm":30,"packageWidthCm":20,"packageHeightCm":40,"packageWeightGrams":1000}' WHERE order_id=$1`, order)
			must(e)
			id := order
			if tc.name == "stale stock" || tc.name == "no stock" {
				id = variant
			}
			_, e = pool.Exec(ctx, tc.setup, id)
			must(e)
			must(flow.Refresh(ctx, pool, []int64{order}))
			s, e := flow.Read(ctx, pool, []int64{order})
			must(e)
			if s[order].Status != "new" || !has(s[order], tc.code) {
				t.Fatalf("blocked order advanced or lacks reason: %+v", s[order])
			}
		})
	}
	t.Run("admin list exposes IDs used by item blockers", func(t *testing.T) {
		orders, e := repo.ListOrders(ctx)
		must(e)
		for _, listed := range orders {
			if listed.ID != order {
				continue
			}
			if len(listed.Items) != 1 || listed.Items[0].ID != orderItem {
				t.Fatalf("admin list lost real item ID %d: %+v", orderItem, listed.Items)
			}
			if len(listed.Workflow.Blockers) != 1 || listed.Workflow.Blockers[0].Code != "dimensions" || !slices.Equal(listed.Workflow.Blockers[0].OrderItemIDs, []int64{listed.Items[0].ID}) {
				t.Fatalf("item ID and blocker ID do not match: %+v", listed.Workflow)
			}
			return
		}
		t.Fatal("fixture order missing from admin list")
	})
	t.Run("same-name rows and repeated variant keep exact item IDs", func(t *testing.T) {
		tx, e := pool.Begin(ctx)
		must(e)
		defer tx.Rollback(ctx)
		_, e = tx.Exec(ctx, `UPDATE orders SET status='new',payment_status='paid' WHERE id=$1`, order)
		must(e)
		_, e = tx.Exec(ctx, `UPDATE inventory SET available_qty=3,synced_at=NOW() WHERE variant_id=$1`, variant)
		must(e)
		_, e = tx.Exec(ctx, `DELETE FROM order_items WHERE order_id=$1`, order)
		must(e)
		var otherVariant int64
		must(tx.QueryRow(ctx, `INSERT INTO product_variants(product_id,label,base_price_minor,is_active) VALUES($1,'D17',10000,1) RETURNING id`, product).Scan(&otherVariant))
		insertItem := func(variantID int64, quantity int, snapshot string) int64 {
			t.Helper()
			var id int64
			must(tx.QueryRow(ctx, `INSERT INTO order_items(order_id,product_id,variant_id,sku,product_name,variant_label,variant_snapshot,unit_price,quantity) SELECT $1,$2,id,sku,'Workflow plant',label,$4::jsonb,100,$5 FROM product_variants WHERE id=$3 RETURNING id`, order, product, variantID, snapshot, quantity).Scan(&id))
			return id
		}
		first := insertItem(variant, 2, `{}`)
		other := insertItem(otherVariant, 1, `{"packageLengthCm":30,"packageWidthCm":20,"packageHeightCm":40,"packageWeightGrams":1000}`)
		repeated := insertItem(variant, 2, `{}`)
		states, e := flow.Read(ctx, tx, []int64{order})
		must(e)
		want := map[string][]int64{"stock_short": {first, repeated}, "dimensions": {first, repeated}, "stock_unknown": {other}}
		if len(states[order].Blockers) != len(want) {
			t.Fatalf("unexpected blockers: %+v", states[order])
		}
		for _, blocker := range states[order].Blockers {
			ids, ok := want[blocker.Code]
			if !ok || !slices.Equal(blocker.OrderItemIDs, ids) {
				t.Fatalf("same-name/grouped variant mapped incorrectly: %+v; expected %v", blocker, ids)
			}
		}
	})
	_, err = pool.Exec(ctx, `UPDATE inventory SET available_qty=3,synced_at=NOW() WHERE variant_id=$1`, variant)
	must(err)
	_, err = pool.Exec(ctx, `UPDATE orders SET status='new',delivery_method='pickup',payment_method='on_delivery',payment_status='on_delivery' WHERE id=$1`, order)
	must(err)
	must(flow.Refresh(ctx, pool, []int64{order}))
	var status string
	must(pool.QueryRow(ctx, `SELECT status FROM orders WHERE id=$1`, order).Scan(&status))
	if status != "new" {
		t.Fatal("unpaid cash auto-advanced")
	}
	for _, next := range []string{"assembling", "ready", "completed"} {
		payment := ""
		if next == "completed" {
			payment = "paid"
		}
		updated, e := repo.UpdateOrderStatus(ctx, actor, order, next, payment)
		err = e
		must(err)
		if len(updated.Items) != 1 || updated.Items[0].ID != orderItem {
			t.Fatalf("status update lost real item ID %d: %+v", orderItem, updated.Items)
		}
	}
	must(pool.QueryRow(ctx, `SELECT status FROM orders WHERE id=$1`, order).Scan(&status))
	if status != "completed" {
		t.Fatal(status)
	}
}

func TestPendingRevisionBlocksAssemblyAndPickupHandover(t *testing.T) {
	for _, status := range []string{"new", "assembling", "ready"} {
		f := readyFacts()
		f.DeliveryMethod = "pickup"
		f.Status = status
		f.RevisionPending = true
		state := flow.Evaluate(f)
		if state.ReadyForAssembly || len(state.ManualStatuses) != 0 || !has(state, "realization_sync") {
			t.Fatal("stale realization allowed fulfilment", status, state)
		}
		f.RevisionPending = false
		settled := flow.Evaluate(f)
		if !settled.ReadyForAssembly {
			t.Fatal("settled revision remained blocked", status, settled)
		}
	}
}
