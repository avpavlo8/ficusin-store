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

// The provider stays in-process; real PostgreSQL validates every live proof,
// revision/attempt boundary and stock-ledger transition.
type amendmentCycleProvider struct {
	origin   func(context.Context, Request, Progress) (Result, error)
	revision func(context.Context, RevisionCommand, Progress) (Result, error)
}

func (p amendmentCycleProvider) Step(ctx context.Context, r Request, s Progress) (Result, error) {
	return p.origin(ctx, r, s)
}
func (p amendmentCycleProvider) StepRevision(ctx context.Context, c RevisionCommand, s Progress) (Result, error) {
	return p.revision(ctx, c, s)
}

func amendmentCycleEvidence(t *testing.T, r Request, p Progress, ids []int64, negative bool) json.RawMessage {
	t.Helper()
	var proof StockProof
	if json.Unmarshal(testLiveProof(t, r, p), &proof) != nil {
		t.Fatal("invalid live fixture proof")
	}
	proof.RevisionID = r.RevisionID
	proof.Posted, proof.HasEntries = !negative, !negative
	if len(ids) == 0 {
		proof.Lines = []StockProofLine{}
	} else {
		if len(ids) != len(proof.Lines) {
			t.Fatal("fixture row ownership mismatch")
		}
		for i, id := range ids {
			proof.Lines[i].ID = id
			proof.Lines[i].Closed = !negative
		}
	}
	raw, err := json.Marshal(proof)
	if err != nil {
		t.Fatal(err)
	}
	return raw
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
	for _, identityCase := range []string{"discover different ID", "discover different UUID", "different ID with incomplete progress", "wrong ID before fill"} {
		t.Run(identityCase, func(t *testing.T) {
			_, before, id := create()
			original, originalHash, base, baseHash := snapshot(id)
			a, err := ApproveRegulationAmendment(ctx, pool, 1, id, verifier)
			if err != nil {
				t.Fatal(err)
			}
			wrongID := a.DocumentID + "9"
			wrongUUID := "ffffffff-ffff-4fff-8fff-ffffffffffff"
			wantID, wantUUID, wantStage := "", "", "discover"
			if identityCase == "wrong ID before fill" {
				wantID, wantUUID, wantStage = wrongID, a.DocumentUUID, "fill"
				exec(`UPDATE order_sale_publications SET document_id=$2,document_uuid=$3,
 progress=jsonb_set(jsonb_set(jsonb_set(progress,'{stage}','"fill"'),'{documentId}',to_jsonb($2::TEXT)),'{documentUUID}',to_jsonb($3::TEXT)) WHERE id=$1`, id, wrongID, a.DocumentUUID)
			}
			calls := 0
			provider := publicationProviderFunc(func(_ context.Context, r Request, p Progress) (Result, error) {
				calls++
				if p.Stage != "discover" || r.OperationKey != before.OperationKey || r.RegulationID != 122498 {
					t.Fatal("unexpected write or identity change", r, p)
				}
				p.DocumentID, p.DocumentUUID, p.Stage = a.DocumentID, a.DocumentUUID, "fill"
				if identityCase == "different ID with incomplete progress" {
					p.DocumentID, p.Stage = wrongID, ""
				} else if identityCase == "discover different ID" {
					p.DocumentID = wrongID
				} else {
					p.DocumentUUID = wrongUUID
				}
				return Result{Progress: p, State: "queued"}, nil
			})
			if worked, err := PublicationStep(ctx, pool, provider, "live"); err != nil || !worked {
				t.Fatal("identity verification did not run", worked, err)
			}
			var raw []byte
			var status, storedID, storedUUID string
			if err := pool.QueryRow(ctx, `SELECT progress,status,document_id,document_uuid FROM order_sale_publications WHERE id=$1`, id).Scan(&raw, &status, &storedID, &storedUUID); err != nil {
				t.Fatal(err)
			}
			var progress Progress
			if json.Unmarshal(raw, &progress) != nil || status != "needs_attention" || !progress.IdentityConflict || progress.DocumentID != wantID || progress.DocumentUUID != wantUUID || progress.Stage != wantStage || storedID != wantID || storedUUID != wantUUID {
				t.Fatal("unapproved native identity was adopted", status, progress, storedID, storedUUID)
			}
			wantCalls := 1
			if identityCase == "wrong ID before fill" {
				wantCalls = 0
			}
			if calls != wantCalls {
				t.Fatal("unapproved native writer was called", calls, wantCalls)
			}
			if worked, err := PublicationStep(ctx, pool, provider, "live"); err != nil || worked || calls != wantCalls {
				t.Fatal("identity conflict reached another provider step", worked, err, calls)
			}
			view, err := loadPublication(ctx, pool, a.IntentID)
			if err != nil || view == nil || view.CanRetry {
				t.Fatal("identity conflict offered retry", view, err)
			}
			after, afterHash, afterBase, afterBaseHash := snapshot(id)
			if after != original || afterHash != originalHash || afterBase != base || afterBaseHash != baseHash {
				t.Fatal("identity conflict rewrote immutable request")
			}
			var approvedID, approvedUUID, approvedHash string
			if err := pool.QueryRow(ctx, `SELECT document_id::TEXT,document_uuid::TEXT,effective_hash FROM order_sale_regulation_amendments WHERE publication_id=$1`, id).Scan(&approvedID, &approvedUUID, &approvedHash); err != nil || approvedID != a.DocumentID || approvedUUID != a.DocumentUUID || approvedHash != a.EffectiveHash {
				t.Fatal("approved identity changed", err, approvedID, approvedUUID)
			}
		})
	}
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
	t.Run("amended posting revision and cancellation retain proofs and release exact stock", func(t *testing.T) {
		event, before, id := create()
		original, hash, base, baseHash := snapshot(id)
		a, e := ApproveRegulationAmendment(ctx, pool, 1, id, verifier)
		if e != nil {
			t.Fatal(e)
		}
		const originalRow, revisedRow int64 = 71001, 71002
		originCalls, revisionCalls, cancellationCalls := 0, 0, 0
		provider := amendmentCycleProvider{}
		provider.origin = func(_ context.Context, r Request, p Progress) (Result, error) {
			originCalls++
			if r.RegulationID != 122498 || r.OperationKey != before.OperationKey {
				t.Fatal("origin not amended", r)
			}
			switch p.Stage {
			case "discover":
				p.DocumentID, p.DocumentUUID, p.Stage = a.DocumentID, a.DocumentUUID, "verify-fill"
				return Result{Progress: p, State: "queued"}, nil
			case "verify-fill":
				p.OwnedRowIDs, p.Stage = []int64{originalRow}, "post"
				return Result{Progress: p, State: "queued"}, nil
			case "post":
				if !p.PostAttempted {
					t.Fatal("origin post attempt was not durable")
				}
				p.Stage = "verified"
				return Result{Progress: p, State: "posted", StockVerified: true,
					StockProof: amendmentCycleEvidence(t, r, p, []int64{originalRow}, false)}, nil
			default:
				t.Fatal("unexpected origin mutation/recreation", p.Stage)
				return Result{}, nil
			}
		}
		provider.revision = func(_ context.Context, c RevisionCommand, p Progress) (Result, error) {
			revisionCalls++
			if c.Origin.RegulationID != 122498 || c.Current.RegulationID != 122498 || c.Target.RegulationID != 122498 ||
				c.Origin.OperationKey != before.OperationKey || c.Target.Event.Lines[0].Quantity != 2 ||
				p.DocumentID != a.DocumentID || p.DocumentUUID != a.DocumentUUID {
				t.Fatal("revision lost amended configuration or document identity", c, p)
			}
			next := func(stage string) (Result, error) {
				p.Stage, p.Unknown = stage, false
				return Result{Progress: p, State: "queued"}, nil
			}
			if p.DesiredState == "cancelled" {
				cancellationCalls++
				switch p.Stage {
				case "verified":
					return next("revision-cancel")
				case "revision-cancel":
					return next("revision-verify-cancel")
				case "revision-verify-cancel":
					if !p.RevisionPostAttempted || !p.RevisionUnpostVerified {
						t.Fatal("cancellation lost the posted revision boundary", p)
					}
					var document StockProof
					json.Unmarshal(amendmentCycleEvidence(t, c.Target, p, []int64{revisedRow}, true), &document)
					proof, _ := json.Marshal(CancellationProof{Kind: "unposted_full", Document: &document})
					p.Stage, p.Unknown = "cancelled", false
					return Result{Progress: p, State: "cancelled", CancellationVerified: true, CancellationProof: proof}, nil
				default:
					t.Fatal("unexpected amended cancellation stage", p.Stage)
				}
			}
			switch p.Stage {
			case "revision-inspect":
				p.RevisionNative = json.RawMessage(`{"sameDocumentRevisionFixture":true}`)
				return next("revision-unpost")
			case "revision-unpost":
				if !p.RevisionUnpostAttempted {
					t.Fatal("unpost attempt was not durable")
				}
				return next("revision-verify-unpost")
			case "revision-verify-unpost":
				current := c.Current
				current.RevisionID = c.RevisionID
				p.RevisionUnpostVerified, p.Stage = true, "revision-delete"
				return Result{Progress: p, State: "queued", RevisionUnpostVerified: true,
					RevisionUnpostProof: amendmentCycleEvidence(t, current, p, []int64{originalRow}, true)}, nil
			case "revision-delete":
				return next("revision-verify-delete")
			case "revision-verify-delete":
				p.ActiveOwnedRowIDs, p.RetiredRowIDs, p.Stage = []int64{}, []int64{originalRow}, "revision-save"
				union, ok := revisionUnionRequest(c)
				if !ok {
					t.Fatal("invalid retirement plan")
				}
				o, _ := requestHash(c.Origin)
				old, _ := requestHash(c.Current)
				target, _ := requestHash(c.Target)
				proof, _ := json.Marshal(RevisionRetirementProof{Version: 1, RevisionID: c.RevisionID,
					OriginHash: o, CurrentHash: old, TargetHash: target, DocumentID: p.DocumentID, DocumentUUID: p.DocumentUUID,
					RetiredRowIDs: []int64{originalRow}, RemainingRowIDs: []int64{},
					Readback: amendmentCycleEvidence(t, union, p, []int64{}, true)})
				return Result{Progress: p, State: "queued", RevisionRetirementProof: proof}, nil
			case "revision-save":
				p.OwnedRowIDs, p.ActiveOwnedRowIDs = []int64{originalRow, revisedRow}, []int64{revisedRow}
				return next("revision-post")
			case "revision-post":
				if !p.RevisionPostAttempted {
					t.Fatal("revision post attempt was not durable")
				}
				return next("revision-verify-post")
			case "revision-verify-post":
				p.Stage = "verified"
				proof := amendmentCycleEvidence(t, c.Target, p, []int64{revisedRow}, false)
				return Result{Progress: p, State: "posted", RevisionVerified: true, StockVerified: true,
					RevisionProof: proof, StockProof: proof}, nil
			default:
				t.Fatal("unexpected amended revision stage", p.Stage)
				return Result{}, nil
			}
		}
		drain := func(wanted string) {
			t.Helper()
			for range 40 {
				var status, message string
				if e := pool.QueryRow(ctx, `SELECT status,message FROM order_sale_publications WHERE id=$1`, id).Scan(&status, &message); e != nil {
					t.Fatal(e)
				}
				if status == wanted {
					return
				}
				if status == "needs_attention" || status == "failed" {
					t.Fatal("amended cycle stopped", status, message)
				}
				if worked, e := PublicationStep(ctx, pool, provider, "live"); e != nil || !worked {
					t.Fatal("amended cycle lost its next step", e)
				}
			}
			t.Fatal("amended cycle exceeded its step bound", wanted)
		}
		drain("posted")
		var originalPositive string
		if e := pool.QueryRow(ctx, `SELECT stock_proof::TEXT FROM order_sale_publications WHERE id=$1`, id).Scan(&originalPositive); e != nil {
			t.Fatal(e)
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
		drain("posted")
		var revisedID, baseID int64
		if e := pool.QueryRow(ctx, `SELECT base_revision_id,applied_revision_id FROM order_sale_intents WHERE id=$1`, event.IntentID).Scan(&baseID, &revisedID); e != nil || revisedID == baseID {
			t.Fatal("revised plan did not post", e)
		}
		tx, e = pool.Begin(ctx)
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
		drain("cancelled")

		type effect struct {
			operation            string
			posted               bool
			quantity, regulation int64
			document, uuid       string
		}
		expected := []effect{{"origin_post", true, 1, 122498, a.DocumentID, a.DocumentUUID},
			{"revision_unpost", false, 1, 122498, a.DocumentID, a.DocumentUUID},
			{"revision_post", true, 2, 122498, a.DocumentID, a.DocumentUUID},
			{"cancel_unpost", false, 0, 122498, a.DocumentID, a.DocumentUUID}}
		rows, e := pool.Query(ctx, `SELECT operation,posted,
 COALESCE((SELECT SUM(quantity) FROM order_sale_revision_effect_lines l WHERE l.effect_id=e.id),0),
 COALESCE(proof->>'regulationId',proof#>>'{document,regulationId}')::BIGINT,
 COALESCE(proof->>'documentId',proof#>>'{document,documentId}'),
 COALESCE(proof->>'documentUUID',proof#>>'{document,documentUUID}')
 FROM order_sale_revision_effects e WHERE intent_id=$1 ORDER BY id`, event.IntentID)
		if e != nil {
			t.Fatal(e)
		}
		count := 0
		for rows.Next() {
			var actual effect
			if e := rows.Scan(&actual.operation, &actual.posted, &actual.quantity, &actual.regulation, &actual.document, &actual.uuid); e != nil {
				rows.Close()
				t.Fatal(e)
			}
			if count >= len(expected) || actual != expected[count] {
				rows.Close()
				t.Fatal("amended ledger/proof diverged", count, actual)
			}
			count++
		}
		e = rows.Err()
		rows.Close()
		if e != nil || count != len(expected) {
			t.Fatal("missing or duplicate financial effect", count, e)
		}
		var heldBefore, availableBefore int64
		if e = pool.QueryRow(ctx, `SELECT COALESCE(SUM(l.quantity-COALESCE((SELECT SUM(r.quantity) FROM order_sale_hold_releases r WHERE r.layer_id=l.id),0)),0) FROM order_sale_hold_layers l WHERE intent_id=$1`, event.IntentID).Scan(&heldBefore); e != nil || heldBefore != 2 {
			t.Fatal("cancel lost hold before fresh native snapshot", heldBefore, e)
		}
		if e = pool.QueryRow(ctx, `SELECT available_qty FROM order_sale_inventory WHERE warehouse_id=1 AND variant_id=88888`).Scan(&availableBefore); e != nil {
			t.Fatal(e)
		}
		exec(`UPDATE inventory SET available_qty=1000,synced_at=clock_timestamp(),stock_observed_at=clock_timestamp() WHERE warehouse_id=1 AND variant_id=88888`)
		var heldAfter, released, owned, availableAfter int64
		if e = pool.QueryRow(ctx, `SELECT COALESCE(SUM(l.quantity-COALESCE((SELECT SUM(r.quantity) FROM order_sale_hold_releases r WHERE r.layer_id=l.id),0)),0),COALESCE(SUM((SELECT SUM(r.quantity) FROM order_sale_hold_releases r WHERE r.layer_id=l.id)),0) FROM order_sale_hold_layers l WHERE intent_id=$1`, event.IntentID).Scan(&heldAfter, &released); e != nil {
			t.Fatal(e)
		}
		if e = pool.QueryRow(ctx, `SELECT order_sale_owned_quantity($1,1,88888),available_qty FROM order_sale_inventory WHERE warehouse_id=1 AND variant_id=88888`, event.OrderID).Scan(&owned, &availableAfter); e != nil {
			t.Fatal(e)
		}
		if heldAfter != 0 || released != 2 || owned != 0 || availableAfter != availableBefore+2 {
			t.Fatal("cancelled amended quantity did not return exactly once", heldAfter, released, owned, availableBefore, availableAfter)
		}
		var publicationReg, baseReg, effectiveReg, originProofReg, revisionProofReg int64
		var finalPositive string
		var cancelled bool
		if e = pool.QueryRow(ctx, `SELECT (p.request->>'regulationId')::BIGINT,(b.request->>'regulationId')::BIGINT,
 (a.effective_request->>'regulationId')::BIGINT,(b.stock_proof->>'regulationId')::BIGINT,(r.stock_proof->>'regulationId')::BIGINT,
 p.stock_proof::TEXT,p.cancellation_verified FROM order_sale_publications p
 JOIN order_sale_regulation_amendments a ON a.publication_id=p.id JOIN order_sale_intents i ON i.id=p.intent_id
 JOIN order_sale_revisions b ON b.id=i.base_revision_id JOIN order_sale_revisions r ON r.id=i.applied_revision_id WHERE p.id=$1`, id).Scan(&publicationReg, &baseReg, &effectiveReg, &originProofReg, &revisionProofReg, &finalPositive, &cancelled); e != nil {
			t.Fatal(e)
		}
		if publicationReg != 122650 || baseReg != 122650 || effectiveReg != 122498 || originProofReg != 122498 || revisionProofReg != 122498 || !cancelled || finalPositive != originalPositive {
			t.Fatal("immutable base or effective positive proof changed", publicationReg, baseReg, effectiveReg, originProofReg, revisionProofReg, cancelled)
		}
		after, afterHash, afterBase, afterBaseHash := snapshot(id)
		if after != original || afterHash != hash || afterBase != base || afterBaseHash != baseHash || originCalls != 3 || revisionCalls < 8 || cancellationCalls != 3 {
			t.Fatal("amended cycle changed origin/history or skipped a boundary", originCalls, revisionCalls, cancellationCalls)
		}
		if worked, e := PublicationStep(ctx, pool, provider, "live"); e != nil || worked {
			t.Fatal("settled cancellation dispatched a second financial operation", worked, e)
		}
	})

}
