package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	ce "github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
	"github.com/avpavlo8/ficusin-store/backend/internal/migrate"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSabyEditorPublicationLocalDatabase(t *testing.T) {
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
	if err = pool.QueryRow(ctx, `SELECT current_database()`).Scan(&db); err != nil {
		t.Fatal(err)
	}
	if db != "ficusin_local_publisher_test" {
		t.Skip("requires isolated publisher test database")
	}
	if err = migrate.Apply(ctx, pool, "../../../timeweb/migrations"); err != nil {
		t.Fatal(err)
	}
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	exec := func(sql string, args ...any) { t.Helper(); _, e := pool.Exec(ctx, sql, args...); must(e) }
	var active int
	must(pool.QueryRow(ctx, `SELECT count(*) FROM catalog_editor_publications WHERE execution_mode='live' AND status IN ('prepared','queued','processing')`).Scan(&active))
	if active != 0 {
		t.Fatal("isolated fixture database must have no pre-existing live jobs")
	}
	tag := fmt.Sprint(time.Now().UnixNano())
	var actorID int64
	must(pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at) VALUES($1,$2,'','Saby editor test',NOW()) RETURNING id`, tag+"@example.invalid", "+7"+tag[len(tag)-10:]).Scan(&actorID))
	actor := Actor{CustomerID: actorID, Role: RoleOwner}
	var products, variants []int64
	defer func() {
		pool.Exec(ctx, `DELETE FROM catalog_editor_remote_links WHERE variant_id=ANY($1::bigint[])`, variants)
		pool.Exec(ctx, `DELETE FROM catalog_editor_publications WHERE variant_id=ANY($1::bigint[])`, variants)
		pool.Exec(ctx, `DELETE FROM admin_audit_log WHERE actor_customer_id=$1`, actorID)
		pool.Exec(ctx, `DELETE FROM products WHERE id=ANY($1::bigint[])`, products)
		pool.Exec(ctx, `DELETE FROM saby_nomenclature WHERE code LIKE $1`, "TEST-SABY-"+tag+"%")
		pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, actorID)
	}()
	newCard := func(mode string) (*PostgresRepository, Product, EditorPlan) {
		t.Helper()
		repo := NewPostgresRepository(pool).WithEditorPublicationMode(mode)
		product, e := repo.CreateProduct(ctx, actor, ProductCreate{Name: "Грунт 5 л " + tag, Description: "Исходное описание", PriceMinor: 149099, CatalogSection: "soil"})
		must(e)
		products = append(products, product.ID)
		rows, e := repo.ListProductVariants(ctx, product.ID)
		must(e)
		if len(rows) != 1 {
			t.Fatal("fixture variant missing")
		}
		variants = append(variants, rows[0].ID)
		return repo, product, EditorPlan{Variants: []EditorVariantPlan{{VariantID: rows[0].ID, Saby: EditorSaby{Mode: "create", Category: EditorCategory{ID: "1041", Name: "Категория из СБИС"}, Folder: EditorCategory{ID: "3333", Name: "Раздел из СБИС"}}}}}
	}
	step := func(publisher ce.Publisher) {
		t.Helper()
		exec(`UPDATE catalog_editor_publications SET next_attempt_at=NOW() WHERE variant_id=ANY($1::bigint[])`, variants)
		must(ce.PublicationStep(ctx, pool, publisher, "live"))
	}
	t.Run("disabled snapshots never run until their own explicit live save", func(t *testing.T) {
		repo, product, plan := newCard("disabled")
		otherRepo, other, otherPlan := newCard("disabled")
		for _, p := range []*EditorPlan{&plan, &otherPlan} {
			p.Variants[0].Marketplace.WB = EditorChannel{Enabled: true, Mode: "link", ExistingID: "fixture-wb" + tag}
			p.Variants[0].Marketplace.Ozon = EditorChannel{Enabled: true, Mode: "link", ExistingID: "fixture-ozon" + tag}
		}
		_, e := repo.SaveEditorPlan(ctx, actor, product.ID, plan, true)
		must(e)
		_, e = otherRepo.SaveEditorPlan(ctx, actor, other.ID, otherPlan, true)
		must(e)
		calls := map[string]int{}
		boundary := editorPublisherFunc(func(_ context.Context, ch string, _ ce.Request, _ ce.Progress) (ce.Result, error) {
			calls[ch]++
			return ce.Result{Status: "failed", Message: "isolated boundary"}, nil
		})
		step(boundary)
		if len(calls) != 0 {
			t.Fatal("enabling live consumed a historical disabled request")
		}
		repo.WithEditorPublicationMode("live")
		_, e = repo.SaveEditorPlan(ctx, actor, product.ID, plan, true)
		must(e)
		for range 4 {
			step(boundary)
		}
		if calls["saby"] != 1 || calls["wb"] != 1 || calls["ozon"] != 1 {
			t.Fatalf("explicit live save did not activate exactly its three channels: %v", calls)
		}
		var stillDisabled int
		must(pool.QueryRow(ctx, `SELECT count(*) FROM catalog_editor_publications WHERE variant_id=$1 AND execution_mode='disabled' AND status='prepared'`, otherPlan.Variants[0].VariantID).Scan(&stillDisabled))
		if stillDisabled != 3 {
			t.Fatal("another disabled card was promoted")
		}
	})
	t.Run("native create receipt then existing update with the same create mode", func(t *testing.T) {
		repo, product, plan := newCard("live")
		_, e := repo.SaveEditorPlan(ctx, actor, product.ID, plan, true)
		must(e)
		variant := plan.Variants[0].VariantID
		publisher := &sabyReceiptFixture{t: t, pool: pool, id: fmt.Sprint(time.Now().UnixNano()), code: "TEST-SABY-" + tag + "-created", uuid: fmt.Sprintf("00000000-0000-4000-8000-%012d", variant)}
		step(publisher)
		step(publisher)
		var canonical string
		must(pool.QueryRow(ctx, `SELECT COALESCE(saby_id,'') FROM product_variants WHERE id=$1`, variant).Scan(&canonical))
		if canonical != "" {
			t.Fatal("unverified native draft became canonical stock identity")
		}
		step(publisher)
		step(publisher)
		must(pool.QueryRow(ctx, `SELECT COALESCE(saby_id,'') FROM product_variants WHERE id=$1`, variant).Scan(&canonical))
		if canonical != publisher.id || publisher.creates != 1 || publisher.updates != 1 {
			t.Fatalf("native receipt not completed exactly once: %s/%+v", canonical, publisher)
		}
		_, e = repo.SaveEditorPlan(ctx, actor, product.ID, plan, true)
		must(e)
		step(publisher)
		if publisher.creates != 1 || publisher.updates != 1 {
			t.Fatal("unchanged repeat Save sent another operation")
		}
		plan.Variants[0].Marketplace.Brand = "Только бренд маркетплейсов"
		plan.Variants[0].OzonPrice = "5500"
		plan.Variants[0].Saby.Category.Name = "Обновлённая подпись того же ID"
		_, e = repo.SaveEditorPlan(ctx, actor, product.ID, plan, true)
		must(e)
		step(publisher)
		if publisher.creates != 1 || publisher.updates != 1 {
			t.Fatal("marketplace-only data replayed a Saby publication")
		}
		exec(`UPDATE products SET description='Изменённое описание' WHERE id=$1`, product.ID)
		_, e = repo.SaveEditorPlan(ctx, actor, product.ID, plan, true)
		must(e)
		var frozen ce.Request
		must(pool.QueryRow(ctx, `SELECT payload FROM catalog_editor_publications WHERE variant_id=$1 AND channel='saby'`, variant).Scan(&frozen))
		if frozen.Action != "update" || frozen.SabyID != publisher.id || frozen.Saby.Mode != "create" {
			t.Fatalf("old create intent discarded canonical identity: %+v", frozen)
		}
		step(publisher)
		step(publisher)
		step(publisher)
		if publisher.creates != 1 || publisher.updates != 2 {
			t.Fatal("editing a linked card created a second remote card")
		}
		var ids, code, uuid int
		must(pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE id_type='id' AND external_id=$2),count(*) FILTER(WHERE id_type='code' AND external_id=$3),count(*) FILTER(WHERE id_type='alias' AND external_id=$4) FROM product_external_ids WHERE variant_id=$1 AND provider='saby'`, variant, publisher.id, publisher.code, publisher.uuid).Scan(&ids, &code, &uuid))
		if ids != 1 || code != 1 || uuid != 1 {
			t.Fatal("numeric identity, business code or UUID were conflated")
		}

	})
	t.Run("unknown create and changed mode keep the irreversible receipt", func(t *testing.T) {
		repo, product, plan := newCard("live")
		_, e := repo.SaveEditorPlan(ctx, actor, product.ID, plan, true)
		must(e)
		variant := plan.Variants[0].VariantID
		publisher := &sabyReceiptFixture{t: t, pool: pool, unknown: true}
		step(publisher)
		step(publisher)
		step(publisher)
		var state string
		var progress ce.Progress
		must(pool.QueryRow(ctx, `SELECT status,progress FROM catalog_editor_publications WHERE variant_id=$1 AND channel='saby'`, variant).Scan(&state, &progress))
		if state != "needs_attention" || !progress.CreateAttempted || !progress.Ambiguous || publisher.creates != 1 {
			t.Fatalf("lost create did not retain manual-only receipt: %s/%+v", state, progress)
		}
		repo.WithEditorPublicationMode("disabled")
		_, e = repo.SaveEditorPlan(ctx, actor, product.ID, plan, true)
		if e == nil {
			t.Fatal("mode change reset an unknown Create receipt")
		}
		must(pool.QueryRow(ctx, `SELECT progress FROM catalog_editor_publications WHERE variant_id=$1 AND channel='saby'`, variant).Scan(&progress))
		if !progress.CreateAttempted || !progress.Ambiguous {
			t.Fatal("unknown Create flags lost")
		}
		repo.WithEditorPublicationMode("live")
		must(repo.RetryEditorPublication(ctx, actor, product.ID, variant, "saby"))
		step(publisher)
		if publisher.creates != 1 {
			t.Fatal("manual retry repeated unknown native Create")
		}
	})
	t.Run("completion waits for editor product lock without retaining variant lock", func(t *testing.T) {
		for _, status := range []string{"queued", "synced"} {
			t.Run(status, func(t *testing.T) {
				repo, product, plan := newCard("live")
				_, e := repo.SaveEditorPlan(ctx, actor, product.ID, plan, true)
				must(e)
				variant := plan.Variants[0].VariantID
				testCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				defer cancel()
				holder, e := pool.Begin(testCtx)
				must(e)
				defer holder.Rollback(ctx)
				var locked int64
				must(holder.QueryRow(testCtx, `SELECT id FROM products WHERE id=$1 FOR UPDATE`, product.ID).Scan(&locked))
				var holderPID int32
				must(holder.QueryRow(testCtx, `SELECT pg_backend_pid()`).Scan(&holderPID))
				remoteReturned := make(chan struct{})
				finished := make(chan error, 1)
				publisher := editorPublisherFunc(func(_ context.Context, ch string, _ ce.Request, p ce.Progress) (ce.Result, error) {
					if ch != "saby" {
						return ce.Result{}, errors.New("unexpected channel")
					}
					p.Stage = "done"
					p.IDs = ce.RemoteIDs{ProductID: fmt.Sprint(time.Now().UnixNano()), OfferID: "TEST-SABY-" + tag + "-lock-" + status, UUID: fmt.Sprintf("00000000-0000-4000-8000-%012d", variant)}
					close(remoteReturned)
					return ce.Result{Status: status, Progress: p}, nil
				})
				go func() { finished <- ce.PublicationStep(testCtx, pool, publisher, "live") }()
				select {
				case <-remoteReturned:
				case <-testCtx.Done():
					t.Fatal("publisher did not reach completion")
				}
				// Observe actual PostgreSQL contention, not a scheduler delay. The
				// blocked completion must not already hold the variant needed by Save.
				for {
					var waiting bool
					must(pool.QueryRow(testCtx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)) AND wait_event_type='Lock')`, holderPID).Scan(&waiting))
					if waiting {
						break
					}
					select {
					case e := <-finished:
						t.Fatalf("completion should wait for product: %v", e)
					case <-testCtx.Done():
						t.Fatal("completion never waited for product lock")
					case <-time.After(10 * time.Millisecond):
					}
				}
				probe, e := pool.Begin(testCtx)
				must(e)
				defer probe.Rollback(ctx)
				must(probe.QueryRow(testCtx, `SELECT id FROM product_variants WHERE id=$1 FOR UPDATE NOWAIT`, variant).Scan(&locked))
				must(probe.Rollback(testCtx))
				must(holder.Commit(testCtx))
				must(<-finished)
				var actualStatus, canonical string
				must(pool.QueryRow(ctx, `SELECT p.status,COALESCE(v.saby_id,'') FROM catalog_editor_publications p JOIN product_variants v ON v.id=p.variant_id WHERE p.variant_id=$1 AND p.channel='saby'`, variant).Scan(&actualStatus, &canonical))
				if actualStatus != status || (status == "synced") != (canonical != "") {
					t.Fatalf("completion lost result after lock wait: %s/%s", actualStatus, canonical)
				}
				// A queued fixture must not be picked up by subsequent subtests.
				exec(`UPDATE catalog_editor_publications SET status='needs_attention' WHERE variant_id=$1 AND channel='saby'`, variant)
			})
		}
	})
	t.Run("expired create lease never reposts after an unknown commit", func(t *testing.T) {
		repo, product, plan := newCard("live")
		_, e := repo.SaveEditorPlan(ctx, actor, product.ID, plan, true)
		must(e)
		variant := plan.Variants[0].VariantID
		publisher := &sabyReceiptFixture{t: t, pool: pool}
		step(publisher)
		// This is the durable state left when a process dies after sending
		// Create, or after receiving its ID but before its result can commit.
		exec(`UPDATE catalog_editor_publications SET status='processing',lease_until=NOW()-INTERVAL '1 second',progress=jsonb_build_object('stage','create','createAttempted',true,'ids','{}'::jsonb) WHERE variant_id=$1 AND channel='saby'`, variant)
		step(publisher)
		var status string
		var progress ce.Progress
		must(pool.QueryRow(ctx, `SELECT status,progress FROM catalog_editor_publications WHERE variant_id=$1 AND channel='saby'`, variant).Scan(&status, &progress))
		if status != "needs_attention" || publisher.creates != 0 || !progress.CreateAttempted || !progress.Ambiguous {
			t.Fatalf("expired native creation was replayed: %s/%+v/%d", status, progress, publisher.creates)
		}
	})
	t.Run("expired native write leases resume readback without replaying POST", func(t *testing.T) {
		for _, recovery := range []struct{ stage, want string }{{"save", "verify"}, {"send-save", "verify"}, {"move", "move-verify"}, {"media", "media-unknown"}} {
			t.Run(recovery.stage, func(t *testing.T) {
				repo, product, plan := newCard("live")
				_, e := repo.SaveEditorPlan(ctx, actor, product.ID, plan, true)
				must(e)
				variant := plan.Variants[0].VariantID
				ids := ce.RemoteIDs{ProductID: fmt.Sprintf("880000%d", variant), OfferID: "TEST-SABY-" + tag + "-" + recovery.stage, UUID: fmt.Sprintf("00000000-0000-4000-8000-%012d", variant)}
				native := map[string]any{"created": true, "images": []any{map[string]any{"source": "https://images.example.test/cover.jpg", "result": map[string]any{"UUID": "durable-image"}}}, "imageIds": []string{"durable-image"}, "prepared": map[string]any{"marker": "durable-native-update"}}
				progress := ce.Progress{Stage: recovery.stage, CreateAttempted: true, IDs: ids}
				progress.Native, _ = json.Marshal(native)
				exec(`UPDATE catalog_editor_publications SET status='processing',lease_until=NOW()-INTERVAL '1 second',progress=$2::jsonb WHERE variant_id=$1 AND channel='saby'`, variant, progress)
				calls, writes := 0, 0
				boundary := editorPublisherFunc(func(_ context.Context, ch string, _ ce.Request, p ce.Progress) (ce.Result, error) {
					calls++
					if ch != "saby" || p.Stage != recovery.want {
						writes++
						t.Fatalf("expired %s re-entered write stage %s", recovery.stage, p.Stage)
					}
					if !p.Ambiguous || p.IDs != ids {
						t.Fatal("native write recovery lost identity/ambiguity proof")
					}
					var actual map[string]any
					if json.Unmarshal(p.Native, &actual) != nil {
						t.Fatal("durable native receipt was corrupted")
					}
					expectedBytes, _ := json.Marshal(native)
					actualBytes, _ := json.Marshal(actual)
					if string(actualBytes) != string(expectedBytes) {
						t.Fatal("native image upload/prepared-update receipts changed during recovery")
					}
					return ce.Result{Progress: p, Status: "needs_attention", Message: "read-only recovery boundary"}, nil
				})
				step(boundary)
				if calls != 1 || writes != 0 {
					t.Fatalf("unexpected recovery callbacks: %d reads/%d writes", calls, writes)
				}
				var canonical string
				must(pool.QueryRow(ctx, `SELECT COALESCE(saby_id,'') FROM product_variants WHERE id=$1`, variant).Scan(&canonical))
				if canonical != "" {
					t.Fatal("unverified recovery changed canonical stock identity")
				}
			})
		}
	})
}

type editorPublisherFunc func(context.Context, string, ce.Request, ce.Progress) (ce.Result, error)

func (f editorPublisherFunc) Advance(ctx context.Context, ch string, r ce.Request, p ce.Progress) (ce.Result, error) {
	return f(ctx, ch, r, p)
}

type sabyReceiptFixture struct {
	t                *testing.T
	pool             *pgxpool.Pool
	id, code, uuid   string
	unknown          bool
	creates, updates int
}

func (f *sabyReceiptFixture) Advance(ctx context.Context, ch string, r ce.Request, p ce.Progress) (ce.Result, error) {
	if ch != "saby" {
		f.t.Fatal("Saby provider received another channel")
	}
	switch p.Stage {
	case "":
		if r.SabyID != "" {
			p.IDs = ce.RemoteIDs{ProductID: r.SabyID, OfferID: f.code, UUID: f.uuid, Barcode: r.Barcode}
			p.Stage = "update"
		} else {
			p.Stage = "create"
		}
		return ce.Result{Progress: p, Status: "queued"}, nil
	case "create":
		var persisted ce.Progress
		var status string
		if e := f.pool.QueryRow(ctx, `SELECT status,progress FROM catalog_editor_publications WHERE variant_id=$1 AND channel='saby'`, r.VariantID).Scan(&status, &persisted); e != nil {
			f.t.Fatal(e)
		}
		if status != "processing" || !persisted.CreateAttempted {
			f.t.Fatal("Create happened before its durable sent boundary")
		}
		f.creates++
		if f.unknown {
			return ce.Result{}, errors.New("simulated lost native Create response")
		}
		if len(r.Barcode) != 13 || r.Barcode[:3] != "200" || r.Saby.Category.ID != "1041" || r.Saby.Folder.ID != "3333" || r.PriceMinor != 149099 || r.Name == "" {
			f.t.Fatalf("incomplete native snapshot: %+v", r)
		}
		p.IDs = ce.RemoteIDs{ProductID: f.id, OfferID: f.code, UUID: f.uuid, SKU: r.SKU, Barcode: r.Barcode}
		p.Stage = "update"
		return ce.Result{Progress: p, Status: "queued"}, nil
	case "update":
		var persisted ce.Progress
		if e := f.pool.QueryRow(ctx, `SELECT progress FROM catalog_editor_publications WHERE variant_id=$1 AND channel='saby'`, r.VariantID).Scan(&persisted); e != nil {
			f.t.Fatal(e)
		}
		if persisted.IDs.ProductID != f.id {
			f.t.Fatal("native draft ID was not durable before Update")
		}
		f.updates++
		p.Stage = "verify"
		return ce.Result{Progress: p, Status: "queued"}, nil
	case "hydrate":
		if p.IDs.ProductID != f.id {
			f.t.Fatal("existing update lost its native ID")
		}
		p.Stage = "update"
		return ce.Result{Progress: p, Status: "queued"}, nil
	case "verify":
		return ce.Result{Progress: p, Status: "synced"}, nil
	case "reconcile":
		return ce.Result{Progress: p, Status: "needs_attention", Message: "unknown native draft requires manual review"}, nil
	}
	b, _ := json.Marshal(p)
	f.t.Fatalf("unexpected fixture stage: %s", b)
	return ce.Result{}, nil
}
