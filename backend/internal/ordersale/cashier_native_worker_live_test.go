package ordersale

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/cashierstock"
	"github.com/jackc/pgx/v5/pgxpool"
)

type cashierWorkerFixture struct {
	pool             *pgxpool.Pool
	t                *testing.T
	mu               sync.Mutex
	requests         []cashierstock.Request
	entered, release chan struct{}
	loseReply        bool
}

func (f *cashierWorkerFixture) Next(ctx context.Context, r cashierstock.Request, p cashierstock.Progress) (cashierstock.Plan, error) {
	if p.Command != nil {
		if !p.Attempted {
			f.t.Error("attempt boundary not retained")
		}
		if p.Error != "" || len(p.Reply) == 0 || string(p.Reply) == "null" {
			return cashierstock.Plan{Native: p.Native, Attention: true, Message: "unknown result retained"}, nil
		}
		return cashierstock.Plan{Native: json.RawMessage(`{"settled":true}`), Done: true}, nil
	}
	params, _ := json.Marshal(r)
	return cashierstock.Plan{Native: json.RawMessage(`{"prepared":true}`), Command: &cashierstock.Command{Kind: "reserve-fixture", Params: params}}, nil
}
func (f *cashierWorkerFixture) Execute(ctx context.Context, c cashierstock.Command) (json.RawMessage, error) {
	var r cashierstock.Request
	if json.Unmarshal(c.Params, &r) != nil {
		return nil, errors.New("bad frozen request")
	}
	var committed bool
	err := f.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM cashier_native_reservations n JOIN cashier_native_attempts a ON a.reservation_id=n.reservation_id
 WHERE n.reservation_id=$1 AND a.command_id=$2::UUID AND n.command_attempted_at IS NOT NULL AND a.command->>'id'=$2::TEXT)`, r.ReservationID, c.ID).Scan(&committed)
	if err != nil || !committed {
		f.t.Errorf("external call preceded durable attempt: %v", err)
	}
	f.mu.Lock()
	f.requests = append(f.requests, r)
	f.mu.Unlock()
	if f.entered != nil {
		close(f.entered)
		<-f.release
	}
	if f.loseReply {
		return nil, errors.New("lost acknowledged remote reply")
	}
	return json.RawMessage(`true`), nil
}

func testCashierNativeWorker(t *testing.T, pool *pgxpool.Pool) {
	ctx := context.Background()
	config := cashierstock.Config{WarehouseID: 283, MovementWarehouseID: 284, OrganizationID: 259, RegulationID: 121326}
	exec := func(t *testing.T, query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	// Other fixtures in this isolated schema belong to earlier lifecycle tests.
	exec(t, `INSERT INTO cashier_native_reservations(reservation_id,status) SELECT id,'needs_attention' FROM cashier_stock_reservations ON CONFLICT DO NOTHING`)
	exec(t, `INSERT INTO product_variants(id,saby_id) VALUES(30001,'3899');INSERT INTO inventory(warehouse_id,variant_id,available_qty,reserved_qty)VALUES(1,30001,10,0)`)
	add := func(t *testing.T, orderID int64) int64 {
		t.Helper()
		exec(t, `INSERT INTO orders(id,order_number,payment_method,payment_status,status,delivery_method)VALUES($1::BIGINT,'NATIVE-CASH-'||$1::TEXT,'on_delivery','pending','new','pickup')`, orderID)
		exec(t, `INSERT INTO order_items VALUES($1,$1,30001,'Native reserve fixture','D13',1,1.00)`, orderID)
		var id int64
		if err := pool.QueryRow(ctx, `SELECT id FROM cashier_stock_reservations WHERE order_id=$1`, orderID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	step := func(t *testing.T, provider cashierstock.Provider) {
		t.Helper()
		worked, err := cashierstock.Step(ctx, pool, provider, config, 18749)
		if err != nil || !worked {
			t.Fatalf("native worker step: worked=%t err=%v", worked, err)
		}
	}
	status := func(t *testing.T, id int64) string {
		t.Helper()
		var s string
		if err := pool.QueryRow(ctx, `SELECT status FROM cashier_native_reservations WHERE reservation_id=$1`, id).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	t.Run("committed command serializes workers and freezes concurrent edits", func(t *testing.T) {
		id := add(t, 30001)
		provider := &cashierWorkerFixture{pool: pool, t: t, entered: make(chan struct{}), release: make(chan struct{})}
		step(t, provider) // durable preparation
		result := make(chan error, 1)
		go func() { _, err := cashierstock.Step(ctx, pool, provider, config, 18749); result <- err }()
		<-provider.entered
		worked, err := cashierstock.Step(ctx, pool, provider, config, 18749)
		if err != nil || worked {
			t.Fatalf("second worker claimed running command: %t %v", worked, err)
		}
		exec(t, `UPDATE order_items SET quantity=2 WHERE id=30001`)
		close(provider.release)
		if err = <-result; err != nil {
			t.Fatal(err)
		}
		provider.entered = nil
		step(t, provider) // read-only verification of the frozen first revision
		if status(t, id) != "settled" || len(provider.requests) != 1 || provider.requests[0].Lines[0].Quantity != 1 {
			t.Fatal("concurrent edit replaced an in-flight request")
		}
		exec(t, `UPDATE cashier_native_reservations SET next_attempt_at=clock_timestamp() WHERE reservation_id=$1`, id)
		step(t, provider)
		step(t, provider)
		step(t, provider)
		if len(provider.requests) != 2 || provider.requests[1].Lines[0].Quantity != 2 || status(t, id) != "settled" {
			t.Fatal("latest composition did not receive its own revision")
		}
		var attempts int
		if err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM cashier_native_attempts WHERE reservation_id=$1 AND reply='true'::JSONB`, id).Scan(&attempts); err != nil || attempts != 2 {
			t.Fatal("immutable attempt receipts missing", attempts, err)
		}
		exec(t, `UPDATE orders SET status='cancelled' WHERE id=30001`)
		exec(t, `UPDATE cashier_native_reservations SET next_attempt_at=clock_timestamp() WHERE reservation_id=$1`, id)
		step(t, provider)
		step(t, provider)
		step(t, provider)
		if provider.requests[2].Terminal != "cancelled" || len(provider.requests[2].Lines) != 0 {
			t.Fatal("cancellation was not frozen as a release request")
		}
		exec(t, `UPDATE cashier_native_reservations SET status='needs_attention' WHERE reservation_id=$1`, id)
	})
	t.Run("lost reply is reconciled without a second execute and history retained", func(t *testing.T) {
		id := add(t, 30002)
		provider := &cashierWorkerFixture{pool: pool, t: t, loseReply: true}
		step(t, provider)
		step(t, provider)
		step(t, provider)
		if status(t, id) != "needs_attention" || len(provider.requests) != 1 {
			t.Fatal("ambiguous native result replayed")
		}
		var retained bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM cashier_native_attempts WHERE reservation_id=$1 AND command IS NOT NULL AND request IS NOT NULL AND error<>'' AND reply='null'::JSONB)`, id).Scan(&retained); err != nil || !retained {
			t.Fatal("unknown operation proof was lost", err)
		}
	})
	t.Run("expired attempt before HTTP is never blindly resent", func(t *testing.T) {
		id := add(t, 30003)
		provider := &cashierWorkerFixture{pool: pool, t: t}
		step(t, provider)
		exec(t, `INSERT INTO cashier_native_attempts(command_id,reservation_id,request,command)
 SELECT (pending_command->>'id')::UUID,reservation_id,request,pending_command FROM cashier_native_reservations WHERE reservation_id=$1`, id)
		exec(t, `UPDATE cashier_native_reservations SET command_attempted_at=clock_timestamp()-INTERVAL '2 minutes',lease_token=gen_random_uuid(),lease_until=clock_timestamp()-INTERVAL '1 second' WHERE reservation_id=$1`, id)
		step(t, provider)
		if len(provider.requests) != 0 || status(t, id) != "needs_attention" {
			t.Fatal("expired unknown command was executed again")
		}
	})
	var physical int
	if err := pool.QueryRow(ctx, `SELECT available_qty FROM inventory WHERE warehouse_id=1 AND variant_id=30001`).Scan(&physical); err != nil || physical != 10 {
		t.Fatal("reservation worker changed physical stock", physical, err)
	}
}
