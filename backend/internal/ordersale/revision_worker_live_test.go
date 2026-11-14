package ordersale

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/avpavlo8/ficusin-store/backend/internal/money"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"testing"
)

type revisionWorkerProvider struct {
	revision func(context.Context, RevisionCommand, Progress) (Result, error)
}

func (p revisionWorkerProvider) Step(context.Context, Request, Progress) (Result, error) {
	return Result{}, fmt.Errorf("unexpected origin call")
}
func (p revisionWorkerProvider) StepRevision(ctx context.Context, c RevisionCommand, s Progress) (Result, error) {
	return p.revision(ctx, c, s)
}
func testRevisionWorker(t *testing.T, pool *pgxpool.Pool) {
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE order_sale_publications SET status='needs_attention' WHERE status='queued';UPDATE inventory SET available_qty=1000,synced_at=NOW() WHERE warehouse_id=1 AND variant_id=1`)
	if err := RefreshDispatchControl(ctx, pool, DispatchControl{Mode: "mock", Enabled: true, SiteWarehouseID: 1}); err != nil {
		t.Fatal(err)
	}
	create := func(id, variant int64) Event {
		t.Helper()
		exec(`INSERT INTO orders(id,order_number,status,delivery_method,cdek_status,subtotal)VALUES($1,$2,'new','pickup','',10)`, id, fmt.Sprintf("REV-%d", id))
		exec(`INSERT INTO order_items VALUES($1,$1,$2,'Before name','D13',1,10)`, id, variant)
		tx, _ := pool.Begin(ctx)
		defer tx.Rollback(ctx)
		event, err := EnqueueCreated(ctx, tx, id)
		if err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		return event
	}
	edit := func(id int64, qty int) {
		t.Helper()
		tx, _ := pool.Begin(ctx)
		defer tx.Rollback(ctx)
		if err := BeforeOrderEdit(ctx, tx, id); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `UPDATE order_items SET quantity=$2 WHERE order_id=$1`, id, qty); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `UPDATE orders SET subtotal=$2::NUMERIC*10 WHERE id=$1`, id, qty); err != nil {
			t.Fatal(err)
		}
		if err := QueueRevision(ctx, tx, id); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
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
		for range 120 {
			if !step(MockProvider{}) {
				return
			}
		}
		t.Fatal("revision pipeline did not settle")
	}
	state := func(e Event) (Progress, string) {
		t.Helper()
		var raw []byte
		var status string
		if err := pool.QueryRow(ctx, `SELECT progress,status FROM order_sale_publications WHERE intent_id=$1`, e.IntentID).Scan(&raw, &status); err != nil {
			t.Fatal(err)
		}
		var p Progress
		if json.Unmarshal(raw, &p) != nil {
			t.Fatal("progress decode")
		}
		return p, status
	}
	reach := func(e Event, wanted string) {
		t.Helper()
		for range 100 {
			p, s := state(e)
			if p.Stage == wanted {
				return
			}
			if s == "needs_attention" || s == "failed" {
				t.Fatalf("wanted %s got %s %+v", wanted, s, p)
			}
			if !step(MockProvider{}) {
				t.Fatalf("wanted %s no work %+v", wanted, p)
			}
		}
		t.Fatal("stage not reached", wanted)
	}
	cancel := func(id int64) {
		t.Helper()
		tx, _ := pool.Begin(ctx)
		defer tx.Rollback(ctx)
		if err := RequestCancellation(ctx, tx, id); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `UPDATE orders SET status='cancelled' WHERE id=$1`, id); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("same document repeated edits retain origin positive proof and audit", func(t *testing.T) {
		e := create(12000, 1)
		drain()
		var hash, doc, proof string
		pool.QueryRow(ctx, `SELECT request_hash,document_id,stock_proof::TEXT FROM order_sale_publications WHERE intent_id=$1`, e.IntentID).Scan(&hash, &doc, &proof)
		edit(12000, 2)
		reach(e, "revision-unpost")
		var activeTarget int64
		pool.QueryRow(ctx, `SELECT active_revision_id FROM order_sale_intents WHERE id=$1`, e.IntentID).Scan(&activeTarget)
		edit(12000, 3)
		step(revisionWorkerProvider{func(_ context.Context, c RevisionCommand, p Progress) (Result, error) {
			if c.RevisionID != activeTarget || c.Target.Event.Lines[0].Quantity != 2 {
				t.Fatal("new desired overwrote active", c)
			}
			return (MockProvider{}).StepRevision(ctx, c, p)
		}})
		drain()
		p, s := state(e)
		var afterHash, afterDoc, afterProof string
		var desired, applied int64
		pool.QueryRow(ctx, `SELECT request_hash,document_id,stock_proof::TEXT FROM order_sale_publications WHERE intent_id=$1`, e.IntentID).Scan(&afterHash, &afterDoc, &afterProof)
		pool.QueryRow(ctx, `SELECT desired_revision_id,applied_revision_id FROM order_sale_intents WHERE id=$1`, e.IntentID).Scan(&desired, &applied)
		if s != "posted" || hash != afterHash || doc != afterDoc || proof != afterProof || desired != applied || len(p.RetiredRowIDs) < 2 {
			t.Fatal("same-document history lost", s, p, desired, applied)
		}
		var original int
		pool.QueryRow(ctx, `SELECT (order_items_snapshot->0->>'quantity')::INTEGER FROM order_sale_revisions WHERE intent_id=$1 ORDER BY revision_number LIMIT 1`, e.IntentID).Scan(&original)
		if original != 1 {
			t.Fatal("audit rewritten")
		}
		cancel(12000)
		drain()
		_, cancelStatus := state(e)
		var verified bool
		var held int64
		pool.QueryRow(ctx, `SELECT cancellation_verified FROM order_sale_publications WHERE intent_id=$1`, e.IntentID).Scan(&verified)
		pool.QueryRow(ctx, `SELECT COALESCE(SUM(l.quantity-COALESCE((SELECT SUM(r.quantity)FROM order_sale_hold_releases r WHERE r.layer_id=l.id),0)),0) FROM order_sale_hold_layers l WHERE intent_id=$1`, e.IntentID).Scan(&held)
		if cancelStatus != "cancelled" || !verified || held != 0 {
			t.Fatal("revised cancellation retained logical stock", cancelStatus, verified, held)
		}

	})
	for n, stage := range []string{"revision-bind", "revision-unpost", "revision-delete", "revision-prepare", "revision-save", "revision-post", "revision-cancel"} {
		t.Run("expired "+stage, func(t *testing.T) {
			id := int64(12100 + n)
			e := create(id, 1)
			drain()
			edit(id, 2)
			if stage == "revision-bind" {
				reach(e, "revision-unpost")
			} else if stage == "revision-cancel" {
				reach(e, "revision-post")
				step(MockProvider{})
				cancel(id)
				reach(e, "revision-cancel")
			} else {
				reach(e, stage)
			}
			p, _ := state(e)
			p.Stage = stage
			p.Unknown = false
			raw, _ := json.Marshal(p)
			exec(`UPDATE order_sale_publications SET status='processing',progress=$2,lease_until=NOW()-INTERVAL'1 second' WHERE intent_id=$1`, e.IntentID, raw)
			if stage == "revision-post" {
				exec(`UPDATE order_sale_revisions SET post_attempted=TRUE WHERE id=$1`, p.TargetRevisionID)
			}
			calls := 0
			step(revisionWorkerProvider{func(_ context.Context, c RevisionCommand, p Progress) (Result, error) {
				calls++
				want := "revision-verify-" + stage[len("revision-"):]
				if p.Stage != want || !p.Unknown {
					t.Fatalf("unsafe recovery %s %+v", want, p)
				}
				return Result{Progress: p, State: "needs_attention", Message: "read-only recovery fixture"}, nil
			}})
			if calls != 1 {
				t.Fatal("recovery not dispatched")
			}
		})
	}
	t.Run("cancel before planned target post never starts it", func(t *testing.T) {
		e := create(12200, 1)
		drain()
		edit(12200, 2)
		reach(e, "revision-post")
		cancel(12200)
		drain()
		p, s := state(e)
		var verified, attempted bool
		pool.QueryRow(ctx, `SELECT cancellation_verified FROM order_sale_publications WHERE intent_id=$1`, e.IntentID).Scan(&verified)
		pool.QueryRow(ctx, `SELECT post_attempted FROM order_sale_revisions WHERE id=$1`, p.TargetRevisionID).Scan(&attempted)
		if s != "cancelled" || !verified || attempted {
			t.Fatal("cancel started new post", s, p, verified, attempted)
		}
	})
	t.Run("missing mapping edit then first admission uses latest plan and saved mode", func(t *testing.T) {
		exec(`INSERT INTO product_variants(id,saby_id)VALUES(12301,NULL),(12302,NULL);INSERT INTO inventory(id,warehouse_id,variant_id,available_qty,reserved_qty,synced_at)VALUES(12302,1,12302,5,0,NOW())`)
		e := create(12300, 12301)
		tx, _ := pool.Begin(ctx)
		if err := BeforeOrderEdit(ctx, tx, 12300); err != nil {
			t.Fatal(err)
		}
		tx.Exec(ctx, `DELETE FROM order_items WHERE order_id=12300`)
		tx.Exec(ctx, `INSERT INTO order_items VALUES(12302,12300,12302,'After name','D24',2,10)`)
		tx.Exec(ctx, `UPDATE orders SET subtotal=20 WHERE id=12300`)
		if err := QueueRevision(ctx, tx, 12300); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		exec(`UPDATE product_variants SET saby_id='3980' WHERE id=12302`)
		current, err := readEvent(ctx, pool, 12300, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = Prepare(ctx, pool, 1, PrepareInput{WarehouseID: 1, Events: []Selection{{OrderID: 12300, Fingerprint: current.Fingerprint}}}); err != nil {
			t.Fatal(err)
		}
		drain()
		p, s := state(e)
		if s != "posted" || p.DocumentID == "" {
			t.Fatal("latest mapped target failed", s, p)
		}
		var raw []byte
		pool.QueryRow(ctx, `SELECT request FROM order_sale_revisions WHERE id=(SELECT applied_revision_id FROM order_sale_intents WHERE id=$1)`, e.IntentID).Scan(&raw)
		var applied Request
		json.Unmarshal(raw, &applied)
		if len(applied.Event.Lines) != 1 || applied.Event.Lines[0].VariantID != 12302 || applied.Event.Lines[0].Quantity != 2 {
			t.Fatal("stale plan published", applied)
		}
		var oldAudit int
		pool.QueryRow(ctx, `SELECT COUNT(*) FROM order_sale_intent_lines WHERE intent_id=$1 AND order_item_id=12300`, e.IntentID).Scan(&oldAudit)
		if oldAudit != 1 {
			t.Fatal("historical item ID deleted")
		}
	})
	t.Run("legacy unknown admission mode cannot silently become live or mock", func(t *testing.T) {
		exec(`INSERT INTO orders(id,order_number,status,delivery_method,cdek_status,subtotal)VALUES(12400,'LEGACY-UNKNOWN','new','pickup','',10);INSERT INTO order_items VALUES(12400,12400,1,'Legacy plant','D13',1,10)`)
		tx, _ := pool.Begin(ctx)
		event, err := readEvent(ctx, tx, 12400, 0)
		if err != nil {
			t.Fatal(err)
		}
		event.Trigger = "created"
		finish(&event)
		raw, _ := json.Marshal(event)
		var intent int64
		if err = tx.QueryRow(ctx, `INSERT INTO order_sale_intents(event_key,order_id,source,snapshot,event_trigger,warehouse_id,origin_config)VALUES($1,12400,'automatic',$2,'created',1,'{"Mode":"disabled","Enabled":false,"SiteWarehouseID":1}')RETURNING id`, event.Key, raw).Scan(&intent); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO order_sale_intent_lines(intent_id,order_item_id,quantity,variant_id)VALUES($1,12400,1,1)`, intent); err != nil {
			t.Fatal(err)
		}
		tx.Commit(ctx)
		current, err := readEvent(ctx, pool, 12400, 0)
		if err != nil {
			t.Fatal(err)
		}
		_, err = Prepare(ctx, pool, 1, PrepareInput{WarehouseID: 1, Events: []Selection{{OrderID: 12400, Fingerprint: current.Fingerprint}}})
		if !errors.Is(err, ErrNotReady) || !strings.Contains(err.Error(), "исходный режим") {
			t.Fatal("legacy mode inferred current settings", err)
		}
		var count int
		pool.QueryRow(ctx, `SELECT COUNT(*) FROM order_sale_publications WHERE intent_id=$1`, intent).Scan(&count)
		if count != 0 {
			t.Fatal("legacy unknown mode created a writer")
		}
	})

	t.Run("new desired revision cannot hide an active canonical mapping conflict", func(t *testing.T) {
		e := create(12500, 1)
		drain()
		edit(12500, 2)
		reach(e, "revision-unpost")
		edit(12500, 3)
		exec(`UPDATE product_variants SET saby_id='other-card' WHERE id=1`)
		calls := 0
		step(revisionWorkerProvider{func(_ context.Context, c RevisionCommand, p Progress) (Result, error) {
			calls++
			return Result{Progress: p, State: "needs_attention"}, nil
		}})
		exec(`UPDATE product_variants SET saby_id='3980' WHERE id=1`)
		_, status := state(e)
		if calls != 0 || status != "needs_attention" {
			t.Fatal("mapping drift hidden by later desired edit", calls, status)
		}
	})

	for n, phase := range []string{"bound", "saved"} {
		t.Run("origin positive ack read barrier "+phase, func(t *testing.T) {
			e := create(int64(12600+n), 1)
			step(MockProvider{})
			step(MockProvider{})
			edit(e.OrderID, 2)
			p, _ := state(e)
			p.Stage = "verify-fill"
			p.Native = []byte(`{"phase":"` + phase + `"}`)
			raw, _ := json.Marshal(p)
			exec(`UPDATE order_sale_publications SET progress=$2 WHERE intent_id=$1`, e.IntentID, raw)
			originCalls := 0
			revisionCalls := 0
			provider := revisionBarrierProvider{origin: func(_ context.Context, r Request, p Progress) (Result, error) {
				originCalls++
				if p.Stage != "verify-fill" {
					t.Fatal("ack barrier left readonly origin stage", p)
				}
				p.Stage = "fill"
				p.Native = []byte(`{"phase":"prepare"}`)
				return Result{Progress: p, State: "queued"}, nil
			}, revision: func(_ context.Context, c RevisionCommand, p Progress) (Result, error) {
				revisionCalls++
				return Result{Progress: p, State: "needs_attention"}, nil
			}}
			step(provider)
			if originCalls != 1 || revisionCalls != 0 {
				t.Fatal("ack resolution bypassed", originCalls, revisionCalls)
			}
			step(provider)
			if revisionCalls != 1 {
				t.Fatal("planned old fill was not preempted after ack", originCalls, revisionCalls)
			}
		})
	}

	t.Run("new discounted order splits native prices but owns one physical item", func(t *testing.T) {
		exec(`INSERT INTO orders(id,order_number,status,delivery_method,cdek_status,subtotal)VALUES(12700,'NEW-NET-299','new','pickup','',299);INSERT INTO order_items VALUES(12700,12700,1,'Discounted plant','D13',3,100)`)
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		event, err := EnqueueCreated(ctx, tx, 12700)
		if err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if len(event.Lines) != 2 {
			t.Fatal("net plan was not split", event.Lines)
		}
		var count, quantity int
		if err = pool.QueryRow(ctx, `SELECT COUNT(*),SUM(quantity)::INTEGER FROM order_sale_intent_lines WHERE intent_id=$1 AND order_item_id=12700 AND variant_id=1`, event.IntentID).Scan(&count, &quantity); err != nil {
			t.Fatal(err)
		}
		if count != 1 || quantity != 3 {
			t.Fatal("physical ownership duplicated", count, quantity)
		}
		drain()
		_, status := state(event)
		if status != "posted" {
			t.Fatal("discounted creation did not publish in mock", status)
		}
		var raw []byte
		if err = pool.QueryRow(ctx, `SELECT stock_proof FROM order_sale_publications WHERE intent_id=$1`, event.IntentID).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var proof StockProof
		if json.Unmarshal(raw, &proof) != nil {
			t.Fatal("proof decode")
		}
		var sum, qty int64
		for _, line := range proof.Lines {
			price, e := money.Parse(string(line.UnitPrice))
			if e != nil {
				t.Fatal(e)
			}
			q, e := line.Quantity.Int64()
			if e != nil {
				t.Fatal(e)
			}
			sum += price * q
			qty += q
		}
		if sum != 29900 || qty != 3 || len(proof.Lines) != 2 || !proof.Test {
			t.Fatal("net proof does not match discounted order", sum, qty, proof)
		}
	})

}

type revisionBarrierProvider struct {
	origin   func(context.Context, Request, Progress) (Result, error)
	revision func(context.Context, RevisionCommand, Progress) (Result, error)
}

func (p revisionBarrierProvider) Step(ctx context.Context, r Request, s Progress) (Result, error) {
	return p.origin(ctx, r, s)
}
func (p revisionBarrierProvider) StepRevision(ctx context.Context, c RevisionCommand, s Progress) (Result, error) {
	return p.revision(ctx, c, s)
}
