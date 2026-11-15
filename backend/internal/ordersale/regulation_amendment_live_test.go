package ordersale

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type amendmentVerifierFunc func(context.Context, Request, Request, Progress) (RegulationAmendmentVerification, error)

func (f amendmentVerifierFunc) VerifyRegulationAmendment(ctx context.Context, before, effective Request, p Progress) (RegulationAmendmentVerification, error) {
	return f(ctx, before, effective, p)
}

func fakeAmendmentVerification(t *testing.T, before, effective Request, p Progress) RegulationAmendmentVerification {
	t.Helper()
	old, _ := requestHash(before)
	next, _ := requestHash(effective)
	ph, _ := hashProgress(p)
	var native map[string]json.RawMessage
	if json.Unmarshal(p.Native, &native) != nil {
		t.Fatal("bad source receipt")
	}
	native["requestHash"], _ = json.Marshal(next)
	p.Native, _ = json.Marshal(native)
	return RegulationAmendmentVerification{Progress: p, Proof: RegulationAmendmentProof{Version: 1, VerifiedAt: time.Now(), BeforeHash: old, EffectiveHash: next, ProgressHash: ph,
		DocumentID: fmt.Sprintf("%d", before.Event.OrderID+100000), DocumentUUID: fmt.Sprintf("00000000-0000-4000-8000-%012d", before.Event.OrderID), PublicUUID: before.OperationKey,
		PublicType: "ДокОтгрИсх", PublicRegulationName: "Реализация", PublicRegulationUUID: "8861254e-4354-11e1-9d64-005056a63fed",
		OrganizationID: effective.OrganizationID, WarehouseID: effective.NativeWarehouseID, MovementWarehouseID: effective.MovementWarehouseID, RegulationID: effective.RegulationID,
		HeaderRead: true, PostingStateKnown: true, LinesRead: true, EmptyLines: true}}
}

func testRegulationAmendments(t *testing.T, pool *pgxpool.Pool) {
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`UPDATE order_sale_publications SET status='needs_attention' WHERE status IN ('queued','processing');
 INSERT INTO customers VALUES(2) ON CONFLICT DO NOTHING;
 INSERT INTO admin_users VALUES(1,'owner',TRUE),(2,'manager',TRUE);
 INSERT INTO product_variants(id,saby_id)VALUES(88888,'3980');
 INSERT INTO inventory(warehouse_id,variant_id,available_qty,reserved_qty,synced_at,stock_observed_at)VALUES(1,88888,1000,0,NOW(),NOW())`)
	oldControl := DispatchControl{Mode: "live", Enabled: true, BuyerID: 41, SiteWarehouseID: 1, NativeWarehouseID: 283, MovementWarehouseID: 284, OrganizationID: 700, RegulationID: 122650}
	currentControl := oldControl
	currentControl.RegulationID = 122498
	setControl := func(c DispatchControl) {
		t.Helper()
		if e := RefreshDispatchControl(ctx, pool, c); e != nil {
			t.Fatal(e)
		}
	}
	var sequence int64 = 98000
	create := func() (Event, Request, int64) {
		t.Helper()
		sequence++
		id := sequence
		setControl(oldControl)
		exec(`INSERT INTO orders(id,order_number,status,delivery_method,cdek_status,payment_method,payment_status)VALUES($1,'AMEND-'||$1::BIGINT::TEXT,'new','pickup','','online','paid')`, id)
		exec(`INSERT INTO order_items VALUES($1,$1,88888,'Amendment plant','D13',1,12.34)`, id)
		tx, e := pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback(ctx)
		event, e := EnqueueCreated(ctx, tx, id)
		if e != nil {
			t.Fatal(e)
		}
		if e = tx.Commit(ctx); e != nil {
			t.Fatal(e)
		}
		var raw []byte
		var publication int64
		if e = pool.QueryRow(ctx, `SELECT id,request FROM order_sale_publications WHERE intent_id=$1`, event.IntentID).Scan(&publication, &raw); e != nil {
			t.Fatal(e)
		}
		var r Request
		if json.Unmarshal(raw, &r) != nil {
			t.Fatal("bad request")
		}
		hash, _ := requestHash(r)
		native, _ := json.Marshal(map[string]any{"version": 1, "requestHash": hash, "phase": "discover", "opaque": "retained", "large": json.Number("9007199254740993123")})
		progress, _ := json.Marshal(Progress{Stage: "discover", DesiredState: "posted", CreateAttempted: true, Native: native})
		exec(`UPDATE order_sale_publications SET progress=$2,status='needs_attention',create_attempted=TRUE,attempts=13 WHERE id=$1`, publication, progress)
		setControl(currentControl)
		return event, r, publication
	}
	verifier := amendmentVerifierFunc(func(_ context.Context, before, effective Request, p Progress) (RegulationAmendmentVerification, error) {
		return fakeAmendmentVerification(t, before, effective, p), nil
	})
	snapshot := func(id int64) (string, string, string, string) {
		t.Helper()
		var request, hash, revision, revisionHash string
		if e := pool.QueryRow(ctx, `SELECT p.request::TEXT,p.request_hash,r.request::TEXT,r.request_hash FROM order_sale_publications p JOIN order_sale_intents i ON i.id=p.intent_id JOIN order_sale_revisions r ON r.id=i.base_revision_id WHERE p.id=$1`, id).Scan(&request, &hash, &revision, &revisionHash); e != nil {
			t.Fatal(e)
		}
		return request, hash, revision, revisionHash
	}
	t.Run("approval is append only and the next claim reads the same effective origin", func(t *testing.T) {
		event, before, id := create()
		oldRequest, oldHash, oldRevision, oldRevisionHash := snapshot(id)
		a, e := ApproveRegulationAmendment(ctx, pool, 1, id, verifier)
		if e != nil {
			t.Fatal(e)
		}
		if a.ID <= 0 || a.BeforeHash != oldHash || a.BeforeHash == a.EffectiveHash {
			t.Fatal("invalid approval", a)
		}
		afterRequest, afterHash, afterRevision, afterRevisionHash := snapshot(id)
		if oldRequest != afterRequest || oldHash != afterHash || oldRevision != afterRevision || oldRevisionHash != afterRevisionHash {
			t.Fatal("amendment rewrote immutable origin")
		}
		var created, posted bool
		var attempts int
		if e = pool.QueryRow(ctx, `SELECT create_attempted,post_attempted,attempts FROM order_sale_publications WHERE id=$1`, id).Scan(&created, &posted, &attempts); e != nil || !created || posted || attempts != 13 {
			t.Fatal("amendment reset attempt history", e)
		}
		for _, sql := range []string{`UPDATE order_sale_regulation_amendments SET effective_hash=repeat('0',64) WHERE publication_id=$1`, `DELETE FROM order_sale_regulation_amendments WHERE publication_id=$1`} {
			if _, e = pool.Exec(ctx, sql, id); e == nil {
				t.Fatal("approval history mutable")
			}
		}
		again, e := ApproveRegulationAmendment(ctx, pool, 1, id, verifier)
		if e != nil || !again.AlreadyApproved || again.ID != a.ID {
			t.Fatal("approval repeated", again, e)
		}
		calls := 0
		provider := publicationProviderFunc(func(_ context.Context, r Request, p Progress) (Result, error) {
			calls++
			if r.RegulationID != 122498 || r.OperationKey != before.OperationKey || !p.CreateAttempted || p.PostAttempted || p.Stage != "discover" {
				t.Fatal("first amended claim was not read-only", r, p)
			}
			hash, _ := requestHash(r)
			var native map[string]json.RawMessage
			json.Unmarshal(p.Native, &native)
			var stored string
			json.Unmarshal(native["requestHash"], &stored)
			if stored != hash {
				t.Fatal("provider metadata was not explicitly rehashed")
			}
			return Result{Progress: p, State: "needs_attention", Message: "read complete"}, nil
		})
		if _, e = PublicationStep(ctx, pool, provider, "live"); e != nil || calls != 1 {
			t.Fatal("effective origin was not used", e, calls)
		}
		if changed, _, _, _ := snapshot(id); changed != oldRequest {
			t.Fatal("claim rewrote original")
		}
		var proofs int
		pool.QueryRow(ctx, `SELECT count(*) FROM order_sale_regulation_amendments WHERE intent_id=$1 AND before_progress->>'stage'='discover' AND effective_progress->>'stage'='discover'`, event.IntentID).Scan(&proofs)
		if proofs != 1 {
			t.Fatal("immutable before/after progress missing")
		}
	})
	for _, mode := range []string{"not owner", "unknown create", "post attempted", "owned rows", "stale proof", "nonempty lines", "posted document", "queued document", "wrong public UUID", "other proof warehouse", "progress race", "config race"} {
		t.Run(mode, func(t *testing.T) {
			_, _, id := create()
			actor := int64(1)
			if mode == "not owner" {
				actor = 2
			}
			if mode == "unknown create" {
				exec(`UPDATE order_sale_publications SET progress=jsonb_set(progress,'{unknown}','true') WHERE id=$1`, id)
			}
			if mode == "post attempted" {
				exec(`UPDATE order_sale_publications SET post_attempted=TRUE WHERE id=$1`, id)
			}
			if mode == "owned rows" {
				exec(`UPDATE order_sale_publications SET progress=jsonb_set(progress,'{ownedRowIds}','[1]') WHERE id=$1`, id)
			}
			checked := false
			bad := amendmentVerifierFunc(func(_ context.Context, before, effective Request, p Progress) (RegulationAmendmentVerification, error) {
				checked = true
				v := fakeAmendmentVerification(t, before, effective, p)
				switch mode {
				case "stale proof":
					v.Proof.VerifiedAt = time.Now().Add(-time.Minute)
				case "nonempty lines":
					v.Proof.EmptyLines = false
				case "posted document":
					v.Proof.Posted = true
				case "queued document":
					v.Proof.PostingQueued = true
				case "wrong public UUID":
					v.Proof.PublicUUID = "ffffffff-ffff-4fff-8fff-ffffffffffff"
				case "other proof warehouse":
					v.Proof.WarehouseID++
				case "progress race":
					exec(`UPDATE order_sale_publications SET progress=jsonb_set(progress,'{unknown}','true') WHERE id=$1`, id)
				case "config race":
					c := currentControl
					c.RegulationID++
					setControl(c)
				}
				return v, nil
			})
			if _, e := ApproveRegulationAmendment(ctx, pool, actor, id, bad); e == nil {
				t.Fatal("unsafe approval accepted", mode)
			}
			var n int
			pool.QueryRow(ctx, `SELECT count(*) FROM order_sale_regulation_amendments WHERE publication_id=$1`, id).Scan(&n)
			if n != 0 {
				t.Fatal("failed approval retained history")
			}
			if (mode == "not owner" || mode == "unknown create" || mode == "post attempted" || mode == "owned rows") && checked {
				t.Fatal("unsafe source reached verifier")
			}
			setControl(currentControl)
		})
	}
	t.Run("cancellation uses approved config without replaying creation", func(t *testing.T) {
		event, before, id := create()
		if _, e := ApproveRegulationAmendment(ctx, pool, 1, id, verifier); e != nil {
			t.Fatal(e)
		}
		tx, e := pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback(ctx)
		if e = RequestCancellation(ctx, tx, event.OrderID); e != nil {
			t.Fatal(e)
		}
		if _, e = tx.Exec(ctx, `UPDATE orders SET status='cancelled' WHERE id=$1`, event.OrderID); e != nil {
			t.Fatal(e)
		}
		if e = tx.Commit(ctx); e != nil {
			t.Fatal(e)
		}
		called := false
		provider := publicationProviderFunc(func(_ context.Context, r Request, p Progress) (Result, error) {
			called = true
			if r.RegulationID != 122498 || r.OperationKey != before.OperationKey || p.DesiredState != "cancelled" || !p.CreateAttempted || p.Stage == "create" {
				t.Fatal("cancel used old config or replayed", r, p)
			}
			return Result{Progress: p, State: "needs_attention", Message: "read-only cancel"}, nil
		})
		if _, e = PublicationStep(ctx, pool, provider, "live"); e != nil || !called {
			t.Fatal("cancel did not receive effective origin", e)
		}
	})
	t.Run("later revisions bind to approved origin and retain the immutable base", func(t *testing.T) {
		event, before, id := create()
		original, hash, base, baseHash := snapshot(id)
		a, e := ApproveRegulationAmendment(ctx, pool, 1, id, verifier)
		if e != nil {
			t.Fatal(e)
		}
		provider := publicationProviderFunc(func(_ context.Context, r Request, p Progress) (Result, error) {
			if r.RegulationID != 122498 || r.OperationKey != before.OperationKey {
				t.Fatal("origin not amended", r)
			}
			switch p.Stage {
			case "discover":
				p.DocumentID = a.DocumentID
				p.DocumentUUID = a.DocumentUUID
				p.Stage = "verify-fill"
				return Result{Progress: p, State: "queued"}, nil
			case "verify-fill":
				p.Stage = "post"
				return Result{Progress: p, State: "queued"}, nil
			case "post":
				p.Stage = "verified"
				return Result{Progress: p, State: "posted", StockVerified: true, StockProof: testLiveProof(t, r, p)}, nil
			default:
				t.Fatal("unexpected stage", p.Stage)
				return Result{}, nil
			}
		})
		for range 3 {
			if _, e = PublicationStep(ctx, pool, provider, "live"); e != nil {
				t.Fatal(e)
			}
		}
		tx, e := pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback(ctx)
		if e = BeforeOrderEdit(ctx, tx, event.OrderID); e != nil {
			t.Fatal(e)
		}
		if _, e = tx.Exec(ctx, `UPDATE order_items SET quantity=2 WHERE order_id=$1`, event.OrderID); e != nil {
			t.Fatal(e)
		}
		if e = QueueRevision(ctx, tx, event.OrderID); e != nil {
			t.Fatal(e)
		}
		if e = tx.Commit(ctx); e != nil {
			t.Fatal(e)
		}
		called := false
		revision := revisionWorkerProvider{revision: func(_ context.Context, c RevisionCommand, p Progress) (Result, error) {
			called = true
			if c.Origin.RegulationID != 122498 || c.Current.RegulationID != 122498 || c.Target.RegulationID != 122498 || c.Origin.OperationKey != before.OperationKey || c.Target.Event.Lines[0].Quantity != 2 {
				t.Fatal("revision lost amended origin", c)
			}
			return Result{Progress: p, State: "needs_attention", Message: "read-only revision"}, nil
		}}
		if _, e = PublicationStep(ctx, pool, revision, "live"); e != nil || !called {
			t.Fatal("revision not bound to effective origin", e)
		}
		after, afterHash, afterBase, afterBaseHash := snapshot(id)
		if after != original || afterHash != hash || afterBase != base || afterBaseHash != baseHash {
			t.Fatal("later revision mutated immutable origin")
		}
	})
}
