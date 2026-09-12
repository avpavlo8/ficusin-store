package procurement

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationCoordinatorLive(t *testing.T) {
	dsn := os.Getenv("CRM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("CRM_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := NewPostgresStore(pool)
	reset := func(channel, resource string) {
		_, err := pool.Exec(ctx, `UPDATE procurement_integration_sync_state SET status='pending',priority='background',requested_generation=0,active_generation=0,completed_generation=0,lease_owner='',lease_until=NULL,last_attempt_at=NULL,last_success_at=NULL,next_attempt_at=CURRENT_TIMESTAMP,next_deep_at=CURRENT_TIMESTAMP+INTERVAL '1 day',cooldown_until=NULL,last_error='' WHERE channel=$1 AND resource=$2`, channel, resource)
		if err != nil {
			t.Fatal(err)
		}
	}
	reset("ozon", "catalog")
	for index := 0; index < 4; index++ {
		if _, err := store.RequestIntegrationSync(ctx, "ozon", "catalog"); err != nil {
			t.Fatal(err)
		}
	}
	claimA, err := store.ClaimIntegrationSync(ctx, "ozon", "catalog", "process-a", time.Minute)
	if err != nil || claimA == nil {
		t.Fatalf("claim A=%+v err=%v", claimA, err)
	}
	for index := 0; index < 5; index++ {
		_, _ = store.RequestIntegrationSync(ctx, "ozon", "catalog")
	}
	var requested, active int64
	if err := pool.QueryRow(ctx, `SELECT requested_generation,active_generation FROM procurement_integration_sync_state WHERE channel='ozon' AND resource='catalog'`).Scan(&requested, &active); err != nil {
		t.Fatal(err)
	}
	if requested != active+1 {
		t.Fatalf("active clicks queued %d generations, want one: active=%d requested=%d", requested-active, active, requested)
	}
	if claim, err := store.ClaimIntegrationSync(ctx, "ozon", "catalog", "process-b", time.Minute); err != nil || claim != nil {
		t.Fatalf("second process claimed active lane: %+v %v", claim, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE procurement_integration_sync_state SET lease_until=CURRENT_TIMESTAMP-INTERVAL '1 second' WHERE channel='ozon' AND resource='catalog'`); err != nil {
		t.Fatal(err)
	}
	claimB, err := store.ClaimIntegrationSync(ctx, "ozon", "catalog", "process-b", time.Minute)
	if err != nil || claimB == nil || claimB.Token <= claimA.Token {
		t.Fatalf("takeover=%+v old=%+v err=%v", claimB, claimA, err)
	}
	now := time.Now().UTC()
	if applied, err := store.FinishIntegrationSync(ctx, *claimA, 1, now, now, nil, 0, nil); err != nil || applied {
		t.Fatalf("stale finisher applied=%v err=%v", applied, err)
	}
	if applied, err := store.FinishIntegrationSync(ctx, *claimB, 2, now, now, nil, 0, nil); err != nil || !applied {
		t.Fatalf("current finisher applied=%v err=%v", applied, err)
	}
	status, err := store.RequestIntegrationSync(ctx, "ozon", "catalog")
	if err != nil {
		t.Fatal(err)
	}
	if status.RequestedGeneration != status.CompletedGeneration+1 {
		t.Fatalf("clicks did not coalesce: %+v", status)
	}

	// A normal completion leaves exactly one follow-up when requests arrive
	// during its active snapshot.
	reset("saby", "catalog")
	first, err := store.ClaimIntegrationSync(ctx, "saby", "catalog", "normal-a", time.Minute)
	if err != nil || first == nil {
		t.Fatal(err)
	}
	for index := 0; index < 5; index++ {
		_, _ = store.RequestIntegrationSync(ctx, "saby", "catalog")
	}
	if applied, err := store.FinishIntegrationSync(ctx, *first, 3, now, now, nil, 0, nil); err != nil || !applied {
		t.Fatalf("normal finish=%v %v", applied, err)
	}
	follow, err := store.ClaimIntegrationSync(ctx, "saby", "catalog", "normal-b", time.Minute)
	if err != nil || follow == nil || follow.Generation != first.Generation+1 {
		t.Fatalf("follow-up=%+v first=%+v err=%v", follow, first, err)
	}
	if applied, err := store.FinishIntegrationSync(ctx, *follow, 4, now, now, nil, 0, nil); err != nil || !applied {
		t.Fatalf("follow-up finish=%v %v", applied, err)
	}
	if extra, err := store.ClaimIntegrationSync(ctx, "saby", "catalog", "normal-c", time.Minute); err != nil || extra != nil {
		t.Fatalf("more than one follow-up: %+v %v", extra, err)
	}

	// A restart reads the stored schedule; it does not infer a deep run from
	// empty process memory.
	if _, err := pool.Exec(ctx, `UPDATE procurement_integration_sync_state SET status='ok',next_attempt_at=CURRENT_TIMESTAMP+INTERVAL '1 hour',next_deep_at=CURRENT_TIMESTAMP+INTERVAL '1 day' WHERE channel='ozon' AND resource='catalog'`); err != nil {
		t.Fatal(err)
	}
	restarted := NewPostgresStore(pool)
	if claim, err := restarted.ClaimIntegrationSync(ctx, "ozon", "catalog", "after-restart", time.Minute); err != nil || claim != nil {
		t.Fatalf("restart relaunched lane: %+v %v", claim, err)
	}

	// Retry-After becomes a persisted account cooldown and another channel is
	// still independently claimable.
	reset("ozon", "sales")
	limited, err := store.ClaimIntegrationSync(ctx, "ozon", "sales", "limited", time.Minute)
	if err != nil || limited == nil {
		t.Fatal(err)
	}
	if applied, err := store.FinishIntegrationSync(ctx, *limited, 0, now, now, nil, 37*time.Second, errors.New("429")); err != nil || !applied {
		t.Fatalf("rate limit finish=%v %v", applied, err)
	}
	if claim, err := store.ClaimIntegrationSync(ctx, "ozon", "sales", "too-early", time.Minute); err != nil || claim != nil {
		t.Fatalf("cooldown ignored: %+v %v", claim, err)
	}
	reset("wb", "catalog")
	reset("ozon", "catalog")
	wb, err := store.ClaimIntegrationSync(ctx, "wb", "catalog", "wb-process", time.Minute)
	if err != nil || wb == nil {
		t.Fatal("WB claim failed", err)
	}
	ozon, err := store.ClaimIntegrationSync(ctx, "ozon", "catalog", "ozon-process", time.Minute)
	if err != nil || ozon == nil {
		t.Fatal("WB blocked Ozon", err)
	}

	// Account and method reservations are shared in PostgreSQL; a server
	// cooldown pauses both the method and the account without blocking WB.
	_, _ = pool.Exec(ctx, `DELETE FROM procurement_integration_rate_limits WHERE channel='ozon'`)
	if delay, err := store.ReserveIntegrationRequest(ctx, "ozon", "v3/product/list", time.Second); err != nil || delay > 100*time.Millisecond {
		t.Fatalf("first reservation delay=%v err=%v", delay, err)
	}
	if delay, err := store.ReserveIntegrationRequest(ctx, "ozon", "v3/posting/fbs/list", time.Second); err != nil || delay < 800*time.Millisecond {
		t.Fatalf("account lane not shared: delay=%v err=%v", delay, err)
	}
	if err := store.DeferIntegrationRequests(ctx, "ozon", "v3/product/list", 30*time.Second); err != nil {
		t.Fatal(err)
	}
	if delay, err := store.IntegrationRequestDelay(ctx, "ozon", "v3/posting/fbs/list"); err != nil || delay < 29*time.Second {
		t.Fatalf("Retry-After not shared: delay=%v err=%v", delay, err)
	}

	// The existing WB mirror has the same fencing guarantee and remains the
	// only worker that owns WB external reads.
	_, err = pool.Exec(ctx, `UPDATE procurement_wb_sync_state SET status='pending',next_attempt_at=CURRENT_TIMESTAMP,locked_until=NULL,lease_owner='' WHERE resource='catalog'`)
	if err != nil {
		t.Fatal(err)
	}
	wbA, err := store.ClaimWBSync(ctx, "catalog", "wb-a", time.Minute)
	if err != nil || wbA == nil {
		t.Fatalf("WB first claim=%+v %v", wbA, err)
	}
	_, err = pool.Exec(ctx, `UPDATE procurement_wb_sync_state SET locked_until=CURRENT_TIMESTAMP-INTERVAL '1 second' WHERE resource='catalog'`)
	if err != nil {
		t.Fatal(err)
	}
	wbB, err := store.ClaimWBSync(ctx, "catalog", "wb-b", time.Minute)
	if err != nil || wbB == nil || wbB.Token <= wbA.Token {
		t.Fatalf("WB takeover=%+v first=%+v %v", wbB, wbA, err)
	}
	if applied, err := store.FinishWBSync(ctx, *wbA, 1, time.Hour, nil); err != nil || applied {
		t.Fatalf("stale WB finisher=%v %v", applied, err)
	}
	if applied, err := store.FinishWBSync(ctx, *wbB, 2, time.Hour, nil); err != nil || !applied {
		t.Fatalf("current WB finisher=%v %v", applied, err)
	}

	// Queue state is isolated from unsaved procurement data.
	var supplierID, orderID int64
	err = pool.QueryRow(ctx, `INSERT INTO procurement_suppliers(name,kind,country_code,default_currency) VALUES($1,'domestic','RU','RUB') RETURNING id`, "CRM sync test "+time.Now().Format(time.RFC3339Nano)).Scan(&supplierID)
	if err != nil {
		t.Fatal(err)
	}
	err = pool.QueryRow(ctx, `INSERT INTO procurement_orders(supplier_id,currency,status,notes) VALUES($1,'RUB','draft','unsaved invoice fields') RETURNING id`, supplierID).Scan(&orderID)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = store.RequestIntegrationSync(ctx, "saby", "catalog")
	var statusValue, notes string
	if err := pool.QueryRow(ctx, `SELECT status,notes FROM procurement_orders WHERE id=$1`, orderID).Scan(&statusValue, &notes); err != nil {
		t.Fatal(err)
	}
	if statusValue != "draft" || notes != "unsaved invoice fields" {
		t.Fatalf("draft changed: %s %q", statusValue, notes)
	}

	// ActionWorker claims are fenced too: a hung process may be replaced, but
	// its late completion cannot overwrite the successor.
	var lineID, batchID, actionID int64
	if err := pool.QueryRow(ctx, `INSERT INTO procurement_order_lines(procurement_order_id,raw_name,ordered_qty) VALUES($1,'Fence test',1) RETURNING id`, orderID).Scan(&lineID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO procurement_action_batches(procurement_order_id,kind,status) VALUES($1,'prices','processing') RETURNING id`, orderID).Scan(&batchID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO procurement_action_items(batch_id,procurement_order_line_id,channel,new_value,status,next_attempt_at) VALUES($1,$2,'saby_price',1,'queued',CURRENT_TIMESTAMP) RETURNING id`, batchID, lineID).Scan(&actionID); err != nil {
		t.Fatal(err)
	}
	actionA, err := store.ClaimAction(ctx, "action-a")
	if err != nil || actionA == nil || actionA.ID != actionID {
		t.Fatalf("first action claim=%+v %v", actionA, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE procurement_action_items SET locked_until=CURRENT_TIMESTAMP-INTERVAL '1 second' WHERE id=$1`, actionID); err != nil {
		t.Fatal(err)
	}
	actionB, err := store.ClaimAction(ctx, "action-b")
	if err != nil || actionB == nil || actionB.LockToken <= actionA.LockToken {
		t.Fatalf("action takeover=%+v first=%+v %v", actionB, actionA, err)
	}
	if applied, err := store.FinishAction(ctx, actionID, actionA.LockOwner, actionA.LockToken, ActionExecution{Completed: true}, nil); err != nil || applied {
		t.Fatalf("stale action finisher=%v %v", applied, err)
	}
	if applied, err := store.FinishAction(ctx, actionID, actionB.LockOwner, actionB.LockToken, ActionExecution{Completed: true}, nil); err != nil || !applied {
		t.Fatalf("current action finisher=%v %v", applied, err)
	}
}
