package ordersale

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"math/big"
	"strconv"
	"strings"
	"testing"
	"time"
)

type publicationProviderFunc func(context.Context, Request, Progress) (Result, error)

func (f publicationProviderFunc) Step(ctx context.Context, r Request, p Progress) (Result, error) {
	return f(ctx, r, p)
}

// Invoked only by the guarded dedicated-schema PostgreSQL harness. Every
// provider below is in-process; no network client is constructed by these tests.
func testPublicationFoundation(t *testing.T, pool *pgxpool.Pool) {
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	create := func(id int64) Event {
		t.Helper()
		exec(`INSERT INTO orders(id,order_number,status,delivery_method,cdek_status)VALUES($1,'SALE-'||$1::BIGINT::TEXT,'completed','pickup','')`, id)
		exec(`INSERT INTO order_items VALUES($1,$1,1,'Frozen plant','D13',1,1234.56)`, id)
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		e, err := Enqueue(ctx, tx, id, 0, "automatic")
		if err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		return e
	}
	control := func(c DispatchControl) {
		t.Helper()
		if err := RefreshDispatchControl(ctx, pool, c); err != nil {
			t.Fatal(err)
		}
	}
	mock := DispatchControl{Mode: "mock", Enabled: true, SiteWarehouseID: 1}
	step := func(provider Provider, mode string) bool {
		t.Helper()
		worked, err := PublicationStep(ctx, pool, provider, mode)
		if err != nil {
			t.Fatal(err)
		}
		return worked
	}
	drain := func() {
		t.Helper()
		for range 50 {
			if !step(MockProvider{}, "mock") {
				return
			}
		}
		t.Fatal("test pipeline did not settle")
	}
	publication := func(e Event) *PublicationView {
		t.Helper()
		p, err := loadPublication(ctx, pool, e.IntentID)
		if err != nil || p == nil {
			t.Fatal("publication missing", err)
		}
		return p
	}
	force := func(e Event, p Progress, status string) {
		t.Helper()
		raw, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		exec(`UPDATE order_sale_publications SET status=$2,progress=$3,lease_until=NOW()-INTERVAL '1 second',next_attempt_at=NOW(),document_id=$4,document_uuid=$5 WHERE intent_id=$1`, e.IntentID, status, raw, p.DocumentID, p.DocumentUUID)
	}
	key := func(e Event) string {
		t.Helper()
		var value string
		if err := pool.QueryRow(ctx, `SELECT request->>'operationKey' FROM order_sale_publications WHERE intent_id=$1`, e.IntentID).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	control(DispatchControl{Mode: "disabled"})
	old := create(700)
	control(mock)
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM order_sale_publications`).Scan(&count); err != nil || count != 0 {
		t.Fatal("activation drained old intents", count, err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Enqueue(ctx, tx, 700, 0, "automatic")
	if err != nil {
		t.Fatal(err)
	}
	tx.Commit(ctx)
	if p, err := loadPublication(ctx, pool, old.IntentID); err != nil || p != nil {
		t.Fatal("repeat milestone drained historical intent", p, err)
	}
	fresh := create(701)
	drain()
	p := publication(fresh)
	if p.Status != "posted" || !p.TestMode || !p.StockVerified || !strings.HasPrefix(p.DocumentID, "STG-SALE-") {
		t.Fatalf("staging did not complete explicitly test-only: %+v", p)
	}
	current, err := readEvent(ctx, pool, 700, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Prepare(ctx, pool, 1, PrepareInput{WarehouseID: 1, Events: []Selection{{OrderID: 700, Fingerprint: current.Fingerprint}}}); err != nil {
		t.Fatal("explicit historical preparation failed", err)
	}
	drain()
	if publication(old).Status != "posted" {
		t.Fatal("explicit historical job was not processed")
	}
	pending := create(702)
	calls := 0
	noLive := publicationProviderFunc(func(context.Context, Request, Progress) (Result, error) {
		calls++
		return Result{}, errors.New("must not run")
	})
	control(DispatchControl{Mode: "live", Enabled: true, BuyerID: 41, SiteWarehouseID: 1, NativeWarehouseID: 283, MovementWarehouseID: 284, OrganizationID: 7, RegulationID: 122650})
	if step(noLive, "live") || calls != 0 {
		t.Fatal("mock job promoted to live")
	}
	if _, err = pool.Exec(ctx, `UPDATE order_sale_publications SET execution_mode='live' WHERE intent_id=$1`, pending.IntentID); err == nil {
		t.Fatal("execution mode was mutable")
	}
	if _, err = pool.Exec(ctx, `UPDATE order_sale_publications SET request=jsonb_set(request,'{siteWarehouseId}','2') WHERE intent_id=$1`, pending.IntentID); err == nil {
		t.Fatal("request was mutable")
	}
	control(mock)
	drain()
	t.Run("missing mapping remains correctable before first publication", func(t *testing.T) {
		exec(`INSERT INTO product_variants VALUES(3,NULL)`)
		exec(`INSERT INTO orders(id,order_number,status,delivery_method,cdek_status)VALUES(711,'SALE-711','completed','pickup','');INSERT INTO order_items VALUES(711,711,3,'Unlinked plant','D13',1,1234.56)`)
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		event, err := Enqueue(ctx, tx, 711, 0, "automatic")
		if err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if event.IntentID == 0 || event.CanPrepare {
			t.Fatal("blocked intent was lost", event)
		}
		if pub, err := loadPublication(ctx, pool, event.IntentID); err != nil || pub != nil {
			t.Fatal("missing mapping froze an invalid publication", pub, err)
		}
		exec(`UPDATE product_variants SET saby_id='3981' WHERE id=3`)
		current, err := readEvent(ctx, pool, 711, 0)
		if err != nil || !current.CanPrepare {
			t.Fatal("fixed mapping not ready", current, err)
		}
		prepared, err := Prepare(ctx, pool, 1, PrepareInput{WarehouseID: 1, Events: []Selection{{OrderID: 711, Fingerprint: current.Fingerprint}}})
		if err != nil || len(prepared) != 1 || prepared[0].Publication == nil {
			t.Fatal("explicit fixed preparation failed", prepared, err)
		}
		drain()
		pub := publication(event)
		if pub.Status != "posted" || !pub.TestMode {
			t.Fatal("fixed mapping not published by test provider", pub)
		}
		var count int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM order_sale_publications WHERE intent_id=$1`, event.IntentID).Scan(&count); err != nil || count != 1 {
			t.Fatal("fixed mapping produced duplicates", count, err)
		}
	})
	t.Run("successful write self loops never repeat a mutation", func(t *testing.T) {
		for index, stage := range []string{"create", "fill", "post"} {
			e := create(int64(712 + index))
			progress := Progress{Stage: stage}
			if stage != "create" {
				progress.DocumentID = fmt.Sprintf("STG-SALE-%d", e.IntentID)
				progress.DocumentUUID = key(e)
			}
			force(e, progress, "queued")
			writes, reads := 0, 0
			fake := publicationProviderFunc(func(_ context.Context, _ Request, p Progress) (Result, error) {
				if p.Stage == stage {
					writes++
					return Result{Progress: p, State: "queued"}, nil
				}
				if p.Stage != "reconcile-"+stage || !p.Unknown {
					t.Fatal("self loop did not switch to read-only", p)
				}
				reads++
				return Result{Progress: p, State: "needs_attention"}, nil
			})
			step(fake, "mock")
			step(fake, "mock")
			pub := publication(e)
			if writes != 1 || stage == "create" && (reads != 0 || pub.CanRetry) || stage != "create" && reads != 1 {
				t.Fatal("acknowledged write replayed", stage, writes, reads, pub)
			}
		}
	})
	t.Run("expired create without saved identity never calls provider or retries", func(t *testing.T) {
		e := create(703)
		force(e, Progress{Stage: "create"}, "processing")
		count := 0
		fake := publicationProviderFunc(func(context.Context, Request, Progress) (Result, error) { count++; return Result{}, nil })
		step(fake, "mock")
		p := publication(e)
		if count != 0 || p.Status != "needs_attention" || p.CanRetry {
			t.Fatal("unknown create was replayed", p, count)
		}
		if RetryPublication(ctx, pool, p.ID) != ErrRetryUnsafe {
			t.Fatal("unknown create exposed unsafe retry")
		}
	})
	t.Run("expired posting reconciles known document read-only", func(t *testing.T) {
		e := create(704)
		progress := Progress{Stage: "post", DocumentID: fmt.Sprintf("STG-SALE-%d", e.IntentID), DocumentUUID: key(e)}
		force(e, progress, "processing")
		calls := 0
		fake := publicationProviderFunc(func(_ context.Context, r Request, p Progress) (Result, error) {
			calls++
			if p.Stage != "reconcile-post" || !p.Unknown {
				t.Fatal("posting replayed", p)
			}
			p.Unknown = false
			return Result{Progress: p, State: "posted", StockVerified: true, StockProof: testMockProof(t, r, p)}, nil
		})
		step(fake, "mock")
		if calls != 1 || publication(e).Status != "posted" {
			t.Fatal("known posting was not reconciled")
		}
	})
	t.Run("lost create reply can never cause another create", func(t *testing.T) {
		e := create(705)
		force(e, Progress{Stage: "create"}, "queued")
		writes := 0
		fake := publicationProviderFunc(func(_ context.Context, r Request, p Progress) (Result, error) {
			writes++
			return Result{Progress: p}, context.DeadlineExceeded
		})
		step(fake, "mock")
		step(fake, "mock")
		if writes != 1 || publication(e).Status != "needs_attention" {
			t.Fatal("create retried after timeout", writes)
		}
	})
	t.Run("posting timeout resumes in read-only recovery stage", func(t *testing.T) {
		e := create(706)
		force(e, Progress{Stage: "post", DocumentID: fmt.Sprintf("STG-SALE-%d", e.IntentID), DocumentUUID: key(e)}, "queued")
		writes, reads := 0, 0
		fake := publicationProviderFunc(func(_ context.Context, r Request, p Progress) (Result, error) {
			if p.Stage == "post" {
				writes++
				return Result{Progress: p}, context.DeadlineExceeded
			}
			if p.Stage != "reconcile-post" || !p.Unknown {
				t.Fatal("unsafe posting retry", p)
			}
			reads++
			p.Unknown = false
			return Result{Progress: p, State: "posted", StockVerified: true, StockProof: testMockProof(t, r, p)}, nil
		})
		step(fake, "mock")
		exec(`UPDATE order_sale_publications SET next_attempt_at=NOW() WHERE intent_id=$1`, e.IntentID)
		step(fake, "mock")
		if writes != 1 || reads != 1 || publication(e).Status != "posted" {
			t.Fatal("posting ambiguity not reconciled", writes, reads)
		}
	})
	t.Run("success needs stock proof and frozen warehouse cannot be retargeted", func(t *testing.T) {
		e := create(707)
		force(e, Progress{Stage: "verify-post", DocumentID: fmt.Sprintf("STG-SALE-%d", e.IntentID), DocumentUUID: key(e)}, "queued")
		step(publicationProviderFunc(func(_ context.Context, r Request, p Progress) (Result, error) {
			return Result{Progress: p, State: "posted"}, nil
		}), "mock")
		p := publication(e)
		if p.Status != "needs_attention" || p.StockVerified {
			t.Fatal("unverified stock success accepted", p)
		}
		original, err := readEvent(ctx, pool, 707, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = Prepare(ctx, pool, 1, PrepareInput{WarehouseID: 2, Events: []Selection{{OrderID: 707, Fingerprint: original.Fingerprint}}}); err != ErrConflict {
			t.Fatal("retargeted immutable job warehouse", err)
		}
	})
	t.Run("native receipt retains exact large numeric fields after JSONB", func(t *testing.T) {
		e := create(708)
		calls := 0
		fake := publicationProviderFunc(func(_ context.Context, r Request, p Progress) (Result, error) {
			calls++
			if calls == 1 {
				p.Stage = "verify-native"
				p.Native = json.RawMessage(`{"id":9007199254740993123,"price":0.123456789012345678901}`)
				return Result{Progress: p, State: "queued"}, nil
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(p.Native, &fields); err != nil {
				t.Fatal(err)
			}
			if string(fields["id"]) != "9007199254740993123" || string(fields["price"]) != "0.123456789012345678901" {
				t.Fatal("native numeric fields rounded", string(p.Native))
			}
			return Result{Progress: p, State: "failed", Message: "test read completed"}, nil
		})
		step(fake, "mock")
		step(fake, "mock")
		if publication(e).Status != "failed" {
			t.Fatal("receipt test did not finish")
		}
	})
	t.Run("safe retry of known unknown posting stays read-only", func(t *testing.T) {
		e := create(710)
		force(e, Progress{Stage: "post", DocumentID: fmt.Sprintf("STG-SALE-%d", e.IntentID), DocumentUUID: key(e), Unknown: true}, "needs_attention")
		view := publication(e)
		if !view.CanRetry {
			t.Fatal("known posting has no safe verification retry")
		}
		if err := RetryPublication(ctx, pool, view.ID); err != nil {
			t.Fatal(err)
		}
		reads := 0
		fake := publicationProviderFunc(func(_ context.Context, r Request, p Progress) (Result, error) {
			if p.Stage != "reconcile-post" || !p.Unknown {
				t.Fatal("retry repeated a mutation", p)
			}
			reads++
			p.Unknown = false
			return Result{Progress: p, State: "posted", StockVerified: true, StockProof: testMockProof(t, r, p)}, nil
		})
		step(fake, "mock")
		if reads != 1 || publication(e).Status != "posted" {
			t.Fatal("safe retry did not verify posting")
		}
	})
	t.Run("late lease reply cannot overwrite recovery", func(t *testing.T) {
		e := create(709)
		fake := publicationProviderFunc(func(_ context.Context, r Request, p Progress) (Result, error) {
			exec(`UPDATE order_sale_publications SET lease_token='newer-owner' WHERE intent_id=$1`, e.IntentID)
			p.Stage = "create"
			return Result{Progress: p, State: "queued"}, nil
		})
		if _, err := PublicationStep(ctx, pool, fake, "mock"); err == nil {
			t.Fatal("stale lease result overwrote recovery")
		}
		var stage string
		if err := pool.QueryRow(ctx, `SELECT progress->>'stage' FROM order_sale_publications WHERE intent_id=$1`, e.IntentID).Scan(&stage); err != nil || stage != "inspect" {
			t.Fatal("lease owner progress lost", stage, err)
		}
	})
	t.Run("one real document cannot satisfy independent intents", func(t *testing.T) {
		control(DispatchControl{Mode: "live", Enabled: true, BuyerID: 41, SiteWarehouseID: 1, NativeWarehouseID: 283, MovementWarehouseID: 284, OrganizationID: 7, RegulationID: 122650})
		first := create(720)
		sharedUUID := "abcdefab-cdef-4abc-8def-abcdefabcdef"
		fake := publicationProviderFunc(func(_ context.Context, r Request, p Progress) (Result, error) {
			p.Stage = "verify-post"
			p.DocumentID = "12345"
			p.DocumentUUID = sharedUUID
			if r.Event.OrderID == 722 {
				p.DocumentID = "54321"
				p.DocumentUUID = strings.ToUpper(sharedUUID)
			}
			p.Native = json.RawMessage(`{"receiptRetained":true}`)
			return Result{Progress: p, State: "posted", StockVerified: true, StockProof: testLiveProof(t, r, p)}, nil
		})
		step(fake, "live")
		original := publication(first)
		if original.Status != "posted" {
			t.Fatal("valid first live fixture proof rejected", original)
		}
		var proofBefore string
		if err := pool.QueryRow(ctx, `SELECT stock_proof::TEXT FROM order_sale_publications WHERE intent_id=$1`, first.IntentID).Scan(&proofBefore); err != nil {
			t.Fatal(err)
		}
		for _, id := range []int64{721, 722} {
			second := create(id)
			step(fake, "live")
			rejected := publication(second)
			if rejected.Status != "needs_attention" || rejected.StockVerified || rejected.DocumentID != "" || rejected.CanRetry {
				t.Fatal("duplicate native identity was claimed", rejected)
			}
			var raw []byte
			if err := pool.QueryRow(ctx, `SELECT progress FROM order_sale_publications WHERE intent_id=$1`, second.IntentID).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			var retained Progress
			if err := json.Unmarshal(raw, &retained); err != nil || !retained.IdentityConflict || retained.DocumentID == "" || len(retained.Native) == 0 || len(retained.ConflictingStockProof) == 0 {
				t.Fatal("conflicting receipt lost audit data", string(raw), err)
			}
		}
		var proofAfter string
		if err := pool.QueryRow(ctx, `SELECT stock_proof::TEXT FROM order_sale_publications WHERE intent_id=$1`, first.IntentID).Scan(&proofAfter); err != nil {
			t.Fatal(err)
		}
		if proofAfter != proofBefore || publication(first).Status != "posted" {
			t.Fatal("first verified receipt was changed")
		}
		control(mock)
	})
	t.Run("queued readback waits without busy polling and bounds delay", func(t *testing.T) {
		control(mock)
		e := create(723)
		calls := 0
		fake := publicationProviderFunc(func(_ context.Context, _ Request, p Progress) (Result, error) {
			calls++
			p.Stage = "verify-delay"
			return Result{Progress: p, State: "queued", RetryAfter: 10 * time.Minute}, nil
		})
		if !step(fake, "mock") || step(fake, "mock") || calls != 1 {
			t.Fatal("readback was polled before its scheduled time", calls)
		}
		var bounded bool
		if err := pool.QueryRow(ctx, `SELECT next_attempt_at>NOW()+INTERVAL '4 minutes' AND next_attempt_at<=NOW()+INTERVAL '5 minutes' FROM order_sale_publications WHERE intent_id=$1`, e.IntentID).Scan(&bounded); err != nil || !bounded {
			t.Fatal("readback delay is not bounded", bounded, err)
		}
	})
}

func testMockProof(t *testing.T, r Request, p Progress) json.RawMessage {
	t.Helper()
	proof, err := mockStockProof(r, p)
	if err != nil {
		t.Fatal(err)
	}
	return proof
}
func testLiveProof(t *testing.T, r Request, p Progress) json.RawMessage {
	t.Helper()
	lines := []map[string]any{}
	for i, line := range r.Event.Lines {
		price, ok := new(big.Rat).SetString(line.UnitPrice)
		if !ok {
			t.Fatal("invalid exact test price")
		}
		sum := new(big.Rat).Mul(price, new(big.Rat).SetInt64(int64(line.Quantity)))
		lines = append(lines, map[string]any{"id": int64(i + 1), "documentId": p.DocumentID, "nomenclatureId": line.SabyID, "warehouseId": r.MovementWarehouseID, "direction": int64(1), "quantity": json.Number(strconv.Itoa(line.Quantity)), "unitPrice": json.Number(line.UnitPrice), "lineSum": json.Number(sum.FloatString(2)), "closed": true})
	}
	proof, err := json.Marshal(map[string]any{"buyerId": r.BuyerID, "version": 1, "test": false, "executionMode": r.ExecutionMode, "intentId": r.IntentID, "eventKey": r.Event.Key, "eventFingerprint": r.Event.Fingerprint, "operationKey": r.OperationKey, "documentId": p.DocumentID, "documentUUID": p.DocumentUUID, "siteWarehouseId": r.SiteWarehouseID, "nativeWarehouseId": r.NativeWarehouseID, "movementWarehouseId": r.MovementWarehouseID, "organizationId": r.OrganizationID, "regulationId": r.RegulationID, "posted": true, "hasEntries": true, "postingErrors": false, "staleEntries": false, "postingQueued": false, "entriesQueued": false, "lines": lines})
	if err != nil {
		t.Fatal(err)
	}
	return proof
}
