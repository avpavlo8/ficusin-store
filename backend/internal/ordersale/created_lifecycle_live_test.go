package ordersale

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"testing"
)

func testCreatedLifecycle(t *testing.T, pool *pgxpool.Pool) {
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	control := func(enabled bool) {
		t.Helper()
		if err := RefreshDispatchControl(ctx, pool, DispatchControl{Mode: "mock", Enabled: enabled, SiteWarehouseID: 1}); err != nil {
			t.Fatal(err)
		}
	}
	create := func(id int64) Event {
		t.Helper()
		exec(`INSERT INTO orders(id,order_number,status,delivery_method,cdek_status) VALUES($1,'EARLY-'||$1::BIGINT::TEXT,'new','cdek','')`, id)
		exec(`INSERT INTO order_items VALUES($1,$1,1,'Frozen unpaid plant','D13',1,12.34)`, id)
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		e, err := EnqueueCreated(ctx, tx, id)
		if err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		return e
	}
	cancel := func(id int64) {
		t.Helper()
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if err = RequestCancellation(ctx, tx, id); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, `UPDATE orders SET status='cancelled' WHERE id=$1`, id); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	step := func(provider Provider) bool {
		t.Helper()
		ok, err := PublicationStep(ctx, pool, provider, "mock")
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	drain := func() {
		t.Helper()
		for range 100 {
			if !step(MockProvider{}) {
				return
			}
		}
		t.Fatal("early lifecycle never settled")
	}
	pub := func(e Event) *PublicationView {
		t.Helper()
		p, err := loadPublication(ctx, pool, e.IntentID)
		if err != nil || p == nil {
			t.Fatal("missing publication", err)
		}
		return p
	}
	exec(`INSERT INTO inventory(id,warehouse_id,variant_id,available_qty,reserved_qty,synced_at)VALUES(9000,1,1,1000,0,NOW())`)
	control(false)
	old := create(9000)
	if old.IntentID != 0 {
		t.Fatal("disabled mode enrolled order")
	}
	control(true)
	var count int
	pool.QueryRow(ctx, `SELECT COUNT(*) FROM order_sale_intents WHERE order_id=9000`).Scan(&count)
	if count != 0 {
		t.Fatal("enabled mode backfilled")
	}
	t.Run("unpaid freezes whole order and suppresses later handover", func(t *testing.T) {
		e := create(9001)
		if e.Trigger != "created" || !e.CanPrepare || e.WarehouseID != 1 || len(e.Lines) != 1 {
			t.Fatalf("bad early cohort %+v", e)
		}
		var variant int64
		pool.QueryRow(ctx, `SELECT variant_id FROM order_sale_intent_lines WHERE intent_id=$1`, e.IntentID).Scan(&variant)
		if variant != 1 {
			t.Fatal("variant not frozen")
		}
		exec(`UPDATE orders SET status='shipped',cdek_status='IN_TRANSIT' WHERE id=9001;INSERT INTO shipment_offers(id,order_id,status,delivery_method,cdek_status)VALUES(9001,9001,'shipped','cdek','IN_TRANSIT')`)
		tx, _ := pool.Begin(ctx)
		again, err := Enqueue(ctx, tx, 9001, 9001, "automatic")
		if err != nil || again.IntentID != e.IntentID {
			t.Fatal("handover duplicated", again, err)
		}
		tx.Commit(ctx)
		drain()
		if pub(e).Status != "posted" || pub(e).StockVerifiedAt == nil {
			t.Fatal("unpaid did not deduct in test", pub(e))
		}
	})
	t.Run("cancel before first claim sends zero provider calls", func(t *testing.T) {
		e := create(9002)
		cancel(9002)
		calls := 0
		step(publicationProviderFunc(func(context.Context, Request, Progress) (Result, error) {
			calls++
			return Result{}, errors.New("must not run")
		}))
		p := pub(e)
		if calls != 0 || p.Status != "cancelled" || !p.CancellationVerified || p.CancellationKind != "no_remote" || p.CancellationVerifiedAt == nil {
			t.Fatal("precreate cancel unsafe", calls, p)
		}
	})
	t.Run("cancel posted preserves original proof and immutable timestamp", func(t *testing.T) {
		e := create(9003)
		drain()
		p := pub(e)
		var hash, proof string
		pool.QueryRow(ctx, `SELECT request_hash,stock_proof::TEXT FROM order_sale_publications WHERE intent_id=$1`, e.IntentID).Scan(&hash, &proof)
		if p.StockVerifiedAt == nil {
			t.Fatal("missing positive timestamp")
		}
		at := *p.StockVerifiedAt
		cancel(9003)
		drain()
		p = pub(e)
		var afterHash, afterProof string
		pool.QueryRow(ctx, `SELECT request_hash,stock_proof::TEXT FROM order_sale_publications WHERE intent_id=$1`, e.IntentID).Scan(&afterHash, &afterProof)
		if p.Status != "cancelled" || !p.CancellationVerified || p.CancellationKind != "unposted_full" || p.CancellationVerifiedAt == nil || p.StockVerifiedAt == nil || !p.StockVerifiedAt.Equal(at) || afterHash != hash || afterProof != proof {
			t.Fatal("reversal lost positive audit", p)
		}
	})
	t.Run("cancel during create resolves known owned document without post", func(t *testing.T) {
		e := create(9004)
		step(MockProvider{})
		calls := 0
		step(publicationProviderFunc(func(_ context.Context, r Request, p Progress) (Result, error) {
			calls++
			cancel(r.Event.OrderID)
			p.DocumentID = fmt.Sprintf("STG-SALE-%d", r.IntentID)
			p.DocumentUUID = r.OperationKey
			p.Stage = "fill"
			return Result{Progress: p, State: "queued"}, nil
		}))
		drain()
		p := pub(e)
		if calls != 1 || p.Status != "cancelled" || p.CancellationKind != "unposted_draft" || p.StockVerified {
			t.Fatal("inflight create cancellation failed", p)
		}
	})
	t.Run("unknown create plus cancellation reaches readonly provider only", func(t *testing.T) {
		e := create(9005)
		step(MockProvider{})
		step(publicationProviderFunc(func(_ context.Context, r Request, p Progress) (Result, error) {
			cancel(r.Event.OrderID)
			return Result{Progress: p}, errors.New("lost create reply")
		}))
		calls := 0
		step(publicationProviderFunc(func(_ context.Context, r Request, p Progress) (Result, error) {
			calls++
			if p.Stage != "inspect-cancel" || !p.CreateAttempted || p.PostAttempted {
				t.Fatal("unsafe unknown-create recovery", p)
			}
			return Result{Progress: p, State: "needs_attention", Message: "Not yet found; no new create"}, nil
		}))
		if calls != 1 || pub(e).Status != "needs_attention" {
			t.Fatal("unknown create retried or skipped")
		}
	})
	t.Run("physical handover requires manual return and cannot retry", func(t *testing.T) {
		e := create(9006)
		exec(`UPDATE orders SET status='shipped',cdek_status='IN_TRANSIT' WHERE id=9006`)
		cancel(9006)
		p := pub(e)
		if p.Status != "needs_attention" || p.CancellationVerified || p.CanRetry {
			t.Fatal("physical stock auto-returned", p)
		}
		if err := RetryPublication(ctx, pool, p.ID); !errors.Is(err, ErrRetryUnsafe) {
			t.Fatal("manual return bypassed", err)
		}
	})
	t.Run("missing mapping still freezes warehouse and releases local hold on cancellation", func(t *testing.T) {
		exec(`INSERT INTO product_variants VALUES(9007,NULL);INSERT INTO orders(id,order_number,status,delivery_method,cdek_status)VALUES(9007,'EARLY-9007','new','pickup','');INSERT INTO order_items VALUES(9007,9007,9007,'Unlinked','D13',1,1.00)`)
		tx, _ := pool.Begin(ctx)
		e, err := EnqueueCreated(ctx, tx, 9007)
		if err != nil {
			t.Fatal(err)
		}
		tx.Commit(ctx)
		if e.CanPrepare || e.WarehouseID != 1 || e.Publication != nil {
			t.Fatal("invalid request frozen", e)
		}
		cancel(9007)
		var verified bool
		pool.QueryRow(ctx, `SELECT cancellation_verified FROM order_sale_intents WHERE id=$1`, e.IntentID).Scan(&verified)
		if !verified {
			t.Fatal("never-created cancelled order still holds stock")
		}
	})
	t.Run("cancel retry cannot repeat acknowledged cancellation", func(t *testing.T) {
		e := create(9008)
		drain()
		p := pub(e)
		var raw []byte
		pool.QueryRow(ctx, `SELECT progress FROM order_sale_publications WHERE id=$1`, p.ID).Scan(&raw)
		var state Progress
		json.Unmarshal(raw, &state)
		state.Stage = "verify-cancel"
		state.DesiredState = "cancelled"
		state.Unknown = true
		raw, _ = json.Marshal(state)
		exec(`UPDATE order_sale_publications SET desired_state='cancelled',status='needs_attention',progress=$2 WHERE id=$1`, p.ID, raw)
		if err := RetryPublication(ctx, pool, p.ID); err != nil {
			t.Fatal(err)
		}
		step(publicationProviderFunc(func(_ context.Context, r Request, p Progress) (Result, error) {
			if p.Stage != "verify-cancel" {
				t.Fatal("cancel retry reset write", p)
			}
			return Result{Progress: p, State: "needs_attention"}, nil
		}))
	})
	t.Run("fresh Saby source is required before the post attempt", func(t *testing.T) {
		e := create(9009)
		step(MockProvider{})
		step(MockProvider{})
		step(MockProvider{})
		exec(`UPDATE saby_nomenclature SET missing_since=NOW() WHERE saby_id='3980'`)
		var rawRequest []byte
		pool.QueryRow(ctx, `SELECT request FROM order_sale_publications WHERE intent_id=$1`, e.IntentID).Scan(&rawRequest)
		var request Request
		json.Unmarshal(rawRequest, &request)
		request.ExecutionMode = "live"
		ready, err := createdStockReady(ctx, pool, request)
		if err != nil || ready {
			t.Fatal("live missing Saby source accepted", ready, err)
		}
		request.ExecutionMode = "mock"
		ready, err = createdStockReady(ctx, pool, request)
		if err != nil || !ready {
			t.Fatal("explicit staging mock required external Saby source", ready, err)
		}
		exec(`UPDATE inventory SET available_qty=0,synced_at=NOW() WHERE warehouse_id=1 AND variant_id=1`)
		calls := 0
		step(publicationProviderFunc(func(context.Context, Request, Progress) (Result, error) {
			calls++
			return Result{}, errors.New("must not post missing source")
		}))
		p := pub(e)
		if calls != 0 || p.Status != "needs_attention" || !p.CanRetry {
			t.Fatal("missing source reached external posting or cannot safely recheck", calls, p)
		}
		exec(`UPDATE saby_nomenclature SET missing_since=NULL WHERE saby_id='3980'`)
	})

}
