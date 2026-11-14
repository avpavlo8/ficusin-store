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
	"github.com/avpavlo8/ficusin-store/backend/internal/procurement"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This exercises the real outbox, locks, formula and stale checks; the adapter
// boundary is deliberately local, so approval tests cannot send remote prices.
func TestEditorPricesLocalDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
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
		t.Skip("isolated publisher database required")
	}
	if err = migrate.Apply(ctx, pool, "../../../timeweb/migrations"); err != nil {
		t.Fatal(err)
	}
	if err = migrate.Apply(ctx, pool, "../../../timeweb/migrations"); err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	exec := func(sql string, args ...any) { t.Helper(); _, e := pool.Exec(ctx, sql, args...); must(e) }
	tag := fmt.Sprint(time.Now().UnixNano())
	var actorID int64
	must(pool.QueryRow(ctx, `INSERT INTO customers(email,phone,full_name,consent_at) VALUES($1,$2,'Price lane test',NOW()) RETURNING id`, tag+"@example.invalid", "+7"+tag[len(tag)-10:]).Scan(&actorID))
	actor := Actor{CustomerID: actorID, Role: RoleOwner}
	repo := NewPostgresRepository(pool).WithEditorPublicationMode("mock")
	var products, variants []int64
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM catalog_editor_price_publications WHERE variant_id=ANY($1::bigint[])`, variants)
		pool.Exec(ctx, `DELETE FROM catalog_editor_publications WHERE variant_id=ANY($1::bigint[])`, variants)
		pool.Exec(ctx, `DELETE FROM catalog_editor_remote_links WHERE variant_id=ANY($1::bigint[])`, variants)
		pool.Exec(ctx, `DELETE FROM admin_audit_log WHERE actor_customer_id=$1`, actorID)
		pool.Exec(ctx, `DELETE FROM products WHERE id=ANY($1::bigint[])`, products)
		pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, actorID)
	})
	fixture := func() (int64, int64, string) {
		t.Helper()
		p, e := repo.CreateProduct(ctx, actor, ProductCreate{Name: "Price test " + tag, PriceMinor: 10000, CatalogSection: "plants"})
		must(e)
		products = append(products, p.ID)
		vs, e := repo.ListProductVariants(ctx, p.ID)
		must(e)
		v := vs[0].ID
		variants = append(variants, v)
		target := fmt.Sprint(900000000 + v)
		exec(`INSERT INTO catalog_editor_remote_links(variant_id,channel,execution_mode,remote_id,offer_id) VALUES($1,'wb','mock',$2,$2)`, v, target)
		return p.ID, v, target
	}
	stage := func(p, v int64, target string, price int64, compare *int64, origin string) EditorPricePublication {
		t.Helper()
		tx, e := pool.Begin(ctx)
		must(e)
		e = stageEditorPrice(ctx, tx, actor, p, v, "wb", target, 10000, price, compare, origin)
		if e != nil {
			tx.Rollback(ctx)
			t.Fatal(e)
		}
		must(tx.Commit(ctx))
		rows, e := repo.EditorPrices(ctx, p)
		must(e)
		if len(rows) == 0 {
			t.Fatal("missing draft")
		}
		return rows[0]
	}
	t.Run("live price receipt cannot block mock content or admission", func(t *testing.T) {
		p, v, target := fixture()
		draft := stage(p, v, target, 25000, nil, "explicit")
		exec(`UPDATE catalog_editor_price_publications SET status='needs_attention',execution_mode='live',approved_at=NOW(),progress='{"stage":"price-verify","ambiguous":true,"native":{"operationId":"live-only-receipt"}}' WHERE id=$1`, draft.ID)
		tx, e := pool.Begin(ctx)
		must(e)
		e = ce.GuardContentPriceConflict(ctx, tx, v, "mock")
		tx.Rollback(ctx)
		must(e)
		request := ce.Request{ProductID: p, VariantID: v, Action: "update", ExistingID: target, Name: "Mock content"}
		raw, e := json.Marshal(request)
		must(e)
		exec(`INSERT INTO catalog_editor_publications(variant_id,channel,fingerprint,payload,status,execution_mode)VALUES($1,'wb','mock-content',$2,'prepared','mock')`, v, raw)
		called := 0
		must(ce.PublicationStep(ctx, pool, editorPublisherFunc(func(_ context.Context, ch string, r ce.Request, s ce.Progress) (ce.Result, error) {
			called++
			if ch != "wb" || r.VariantID != v {
				t.Fatal("wrong mock content")
			}
			return ce.Result{Progress: s, Status: "synced"}, nil
		}), "mock"))
		if called != 1 {
			t.Fatal("cross-mode price blocked mock content")
		}
		var status string
		var progress ce.Progress
		must(pool.QueryRow(ctx, `SELECT status,progress FROM catalog_editor_price_publications WHERE id=$1`, draft.ID).Scan(&status, &progress))
		if status != "needs_attention" || !progress.Ambiguous || len(progress.Native) == 0 {
			t.Fatal("unrelated live receipt was changed")
		}
	})
	t.Run("approved price drains before sync-existing admits changed content", func(t *testing.T) {
		p, v, target := fixture()
		draft := stage(p, v, target, 26000, nil, "explicit")
		request := ce.Request{ProductID: p, VariantID: v, Action: "update", ExistingID: target, Name: "Original content", Description: "Original description"}
		raw, e := json.Marshal(request)
		must(e)
		exec(`INSERT INTO catalog_editor_publications(variant_id,channel,fingerprint,payload,status,execution_mode,progress)VALUES($1,'wb',$2,$3,'synced','mock','{"native":{"preserved":"content-receipt"}}')`, v, editorPublicationFingerprint("wb", raw), raw)
		_, e = repo.ApproveEditorPrice(ctx, Actor{CustomerID: actorID, Role: RoleManager}, p, draft.ID, draft.Fingerprint)
		must(e)
		exec(`UPDATE products SET name='Changed saved content' WHERE id=$1`, p)
		sync, e := repo.SyncExistingEditorPlan(ctx, Actor{CustomerID: actorID, Role: RoleAdministrator}, p)
		must(e)
		if sync.Queued != 0 || sync.Skipped != 1 || len(sync.Warnings) != 1 {
			t.Fatalf("price/content admission did not explain retry: %+v", sync)
		}
		var status string
		var contentRaw []byte
		var receipt ce.Progress
		must(pool.QueryRow(ctx, `SELECT status,payload,progress FROM catalog_editor_publications WHERE variant_id=$1 AND channel='wb'`, v).Scan(&status, &contentRaw, &receipt))
		if status != "synced" || string(contentRaw) == "" || len(receipt.Native) == 0 {
			t.Fatal("sync-existing overwrote a completed content receipt")
		}
		pricesSent := 0
		must(ce.PricePublicationStep(ctx, pool, editorPublisherFunc(func(_ context.Context, ch string, r ce.Request, s ce.Progress) (ce.Result, error) {
			pricesSent++
			if r.Action != "price" || r.VariantID != v {
				t.Fatal("wrong approved price")
			}
			return ce.Result{Status: "synced", Progress: s}, nil
		}), "mock"))
		if pricesSent != 1 {
			t.Fatal("price worker remained blocked by newly queued content")
		}
		sync, e = repo.SyncExistingEditorPlan(ctx, Actor{CustomerID: actorID, Role: RoleAdministrator}, p)
		must(e)
		if sync.Queued != 1 {
			t.Fatalf("explicit retry did not admit saved content: %+v", sync)
		}
		contentSent := 0
		must(ce.PublicationStep(ctx, pool, editorPublisherFunc(func(_ context.Context, ch string, r ce.Request, s ce.Progress) (ce.Result, error) {
			contentSent++
			if r.Name != "Changed saved content" {
				t.Fatal("lost saved content")
			}
			return ce.Result{Progress: s, Status: "synced"}, nil
		}), "mock"))
		if contentSent != 1 {
			t.Fatal("saved content could not drain after price")
		}
	})
	t.Run("explicit approval, content conflict and durable terminal receipt", func(t *testing.T) {
		p, v, target := fixture()
		draft := stage(p, v, target, 25000, nil, "explicit")
		if draft.Status != "draft" || !draft.CanApprove {
			t.Fatalf("draft not approvable: %+v", draft)
		}
		called := 0
		boundary := editorPublisherFunc(func(_ context.Context, ch string, r ce.Request, s ce.Progress) (ce.Result, error) {
			called++
			if ch != "wb" || r.Action != "price" || r.Price.PriceMinor != 25000 {
				t.Fatal("wrong immutable command")
			}
			return ce.Result{Status: "synced", Progress: s}, nil
		})
		must(ce.PricePublicationStep(ctx, pool, boundary, "mock"))
		if called != 0 {
			t.Fatal("draft auto-sent")
		}
		_, e := repo.ApproveEditorPrice(ctx, Actor{CustomerID: actorID, Role: RoleAdministrator}, p, draft.ID, draft.Fingerprint)
		if !errors.Is(e, ErrForbidden) {
			t.Fatalf("role without product management approved: %v", e)
		}
		_, e = repo.ApproveEditorPrice(ctx, actor, p, draft.ID, "stale")
		if !errors.Is(e, pgx.ErrNoRows) {
			t.Fatalf("stale fingerprint accepted: %v", e)
		}
		// A completed create receipt keeps createAttempted=true permanently.
		exec(`INSERT INTO catalog_editor_publications(variant_id,channel,fingerprint,payload,status,execution_mode,progress) VALUES($1,'wb','content','{}','created','mock','{"createAttempted":true}')`, v)
		_, e = repo.ApproveEditorPrice(ctx, actor, p, draft.ID, draft.Fingerprint)
		must(e)
		_, e = repo.ApproveEditorPrice(ctx, actor, p, draft.ID, draft.Fingerprint)
		if !errors.Is(e, pgx.ErrNoRows) {
			t.Fatalf("double approval accepted: %v", e)
		}
		tx, e := pool.Begin(ctx)
		must(e)
		e = ce.GuardContentPriceConflict(ctx, tx, v, "mock")
		tx.Rollback(ctx)
		if e == nil {
			t.Fatal("content overwrote approved price")
		}
		exec(`UPDATE catalog_editor_publications SET status='failed',progress='{}' WHERE variant_id=$1 AND channel='wb'`, v)
		if e = repo.RetryEditorPublication(ctx, actor, p, v, "wb"); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("content retry bypassed approved price: %v", e)
		}
		// The claim boundary also protects old or independently queued content.
		exec(`UPDATE catalog_editor_publications SET status='prepared',next_attempt_at=NOW() WHERE variant_id=$1 AND channel='wb'`, v)
		must(ce.PublicationStep(ctx, pool, editorPublisherFunc(func(context.Context, string, ce.Request, ce.Progress) (ce.Result, error) {
			t.Fatal("content claim bypassed active price")
			return ce.Result{}, nil
		}), "mock"))
		exec(`UPDATE catalog_editor_publications SET status='created',progress='{"createAttempted":true}' WHERE variant_id=$1 AND channel='wb'`, v)
		must(ce.PricePublicationStep(ctx, pool, boundary, "mock"))
		if called != 1 {
			t.Fatal("approved command not executed exactly once")
		}
		var fingerprint, status string
		must(pool.QueryRow(ctx, `SELECT fingerprint,status FROM catalog_editor_publications WHERE variant_id=$1 AND channel='wb'`, v).Scan(&fingerprint, &status))
		if fingerprint != "content" || status != "created" {
			t.Fatal("price lane overwrote content receipt")
		}
	})
	t.Run("site-only scope cancels drafts and retains unresolved native receipts", func(t *testing.T) {
		p, v, target := fixture()
		draft := stage(p, v, target, 25000, nil, "explicit")
		must(repo.SetVariantMarketplaceScope(ctx, actor, v, "site_only"))
		var status string
		must(pool.QueryRow(ctx, `SELECT status FROM catalog_editor_price_publications WHERE id=$1`, draft.ID).Scan(&status))
		if status != "superseded" {
			t.Fatal("disabled scope left draft")
		}
		must(repo.SetVariantMarketplaceScope(ctx, actor, v, "marketplaces"))
		draft = stage(p, v, target, 26000, nil, "explicit")
		_, e := repo.ApproveEditorPrice(ctx, actor, p, draft.ID, draft.Fingerprint)
		must(e)
		exec(`UPDATE catalog_editor_price_publications SET status='needs_attention',progress='{"stage":"price-verify","ambiguous":true,"native":{"operationId":"scope-receipt"}}' WHERE id=$1`, draft.ID)
		must(repo.SetVariantMarketplaceScope(ctx, actor, v, "site_only"))
		var progress ce.Progress
		must(pool.QueryRow(ctx, `SELECT status,progress FROM catalog_editor_price_publications WHERE id=$1`, draft.ID).Scan(&status, &progress))
		if status != "needs_attention" || len(progress.Native) == 0 {
			t.Fatal("scope discarded sent receipt")
		}
		rows, e := repo.EditorPrices(ctx, p)
		must(e)
		if !rows[0].CanRetry {
			t.Fatal("disabled scope prevents readback")
		}
		_, e = repo.RetryEditorPrice(ctx, actor, p, draft.ID, draft.Fingerprint)
		must(e)
		must(ce.PricePublicationStep(ctx, pool, editorPublisherFunc(func(_ context.Context, _ string, _ ce.Request, s ce.Progress) (ce.Result, error) {
			if s.Stage != "price-verify" || len(s.Native) == 0 {
				t.Fatal("scope recheck repeated write")
			}
			return ce.Result{Status: "synced", Progress: s}, nil
		}), "mock"))
	})
	t.Run("site and formula changes supersede before sending", func(t *testing.T) {
		p, v, target := fixture()
		draft := stage(p, v, target, 25000, nil, "explicit")
		_, e := repo.ApproveEditorPrice(ctx, actor, p, draft.ID, draft.Fingerprint)
		must(e)
		exec(`UPDATE product_variants SET base_price_minor=10001 WHERE id=$1`, v)
		boundary := editorPublisherFunc(func(context.Context, string, ce.Request, ce.Progress) (ce.Result, error) {
			t.Fatal("stale command sent")
			return ce.Result{}, nil
		})
		must(ce.PricePublicationStep(ctx, pool, boundary, "mock"))
		var status string
		must(pool.QueryRow(ctx, `SELECT status FROM catalog_editor_price_publications WHERE id=$1`, draft.ID).Scan(&status))
		if status != "superseded" {
			t.Fatal(status)
		}
		exec(`UPDATE product_variants SET base_price_minor=10000,current_unit_cost_rub=100 WHERE id=$1`, v)
		for _, code := range []string{"package_length_cm", "package_width_cm", "package_height_cm", "package_winter_length_cm", "package_winter_width_cm", "package_winter_height_cm"} {
			exec(`INSERT INTO variant_attribute_values(variant_id,attribute_id,value) SELECT $1,id,'20'::jsonb FROM attribute_definitions WHERE code=$2 ON CONFLICT(variant_id,attribute_id) DO UPDATE SET value=EXCLUDED.value`, v, code)
		}
		var height, pack, logistics, returns, market, tax, reserve, strike float64
		must(pool.QueryRow(ctx, `SELECT COALESCE(variant_numeric_attribute(v.id,'height_cm'),0)::float8,s.package_rub::float8,s.marketplace_logistics_per_cm::float8,s.return_loss_rate::float8,s.marketplace_cost_rate::float8,s.tax_rate::float8,s.reserve_rate::float8,s.marketplace_strike_markup::float8 FROM product_variants v CROSS JOIN procurement_pricing_settings s WHERE v.id=$1 AND s.id=1`, v).Scan(&height, &pack, &logistics, &returns, &market, &tax, &reserve, &strike))
		price, compare := procurement.LegacyMarketplacePrices(10000, pack, height, logistics, returns, market, tax, reserve, strike)
		quotes, enabled, e := procurement.UnifiedVariantPriceQuotes(ctx, pool, v)
		must(e)
		if enabled {
			if quotes["wb"].PriceRUB == nil {
				t.Fatal("fixture requires a valid WB formula")
			}
			price = *quotes["wb"].PriceRUB
			compare = procurement.CompareAtPrice(price, strike)
		}
		compareMinor := compare * 100
		draft = stage(p, v, target, price*100, &compareMinor, "formula")
		if !draft.CanApprove {
			t.Fatalf("current formula rejected: %+v", draft)
		}
		tx, e := pool.Begin(ctx)
		must(e)
		defer tx.Rollback(ctx)
		_, e = tx.Exec(ctx, `UPDATE procurement_pricing_settings SET marketplace_strike_markup=CASE WHEN marketplace_strike_markup<0.5 THEN 0.75 ELSE 0.25 END WHERE id=1`)
		must(e)
		var req ce.Request
		must(tx.QueryRow(ctx, `SELECT payload FROM catalog_editor_price_publications WHERE id=$1`, draft.ID).Scan(&req))
		reason, e := ce.PriceSnapshotCurrent(ctx, tx, req, "wb", "mock", "formula")
		must(e)
		if reason == "" {
			t.Fatal("changed strike formula accepted")
		}
	})
	t.Run("automatic full save retains formula guard while manual save freezes explicit value", func(t *testing.T) {
		p, v, _ := fixture()
		sabyID := fmt.Sprint(870000000 + v)
		exec(`INSERT INTO saby_nomenclature(saby_id,code) VALUES($1,$2)`, sabyID, "auto-price-"+tag)
		exec(`UPDATE product_variants SET saby_id=$2,current_unit_cost_rub=100 WHERE id=$1`, v, sabyID)
		for _, code := range []string{"package_length_cm", "package_width_cm", "package_height_cm", "package_winter_length_cm", "package_winter_width_cm", "package_winter_height_cm"} {
			exec(`INSERT INTO variant_attribute_values(variant_id,attribute_id,value) SELECT $1,id,'20'::jsonb FROM attribute_definitions WHERE code=$2 ON CONFLICT(variant_id,attribute_id) DO UPDATE SET value=EXCLUDED.value`, v, code)
		}
		var strike float64
		must(pool.QueryRow(ctx, `SELECT marketplace_strike_markup::float8 FROM procurement_pricing_settings WHERE id=1`).Scan(&strike))
		t.Cleanup(func() {
			pool.Exec(ctx, `UPDATE procurement_pricing_settings SET marketplace_strike_markup=$1 WHERE id=1`, strike)
			pool.Exec(ctx, `DELETE FROM saby_nomenclature WHERE saby_id=$1`, sabyID)
		})
		price, _, valid, e := ce.PriceFormula(ctx, pool, v, "wb", 10000)
		must(e)
		if !valid {
			t.Fatal("fixture formula unavailable")
		}
		plan := EditorPlan{Variants: []EditorVariantPlan{{VariantID: v, PriceMode: "auto", WBPrice: fmt.Sprintf("%.2f", float64(price)/100)}}}
		_, e = repo.SaveEditorPlan(ctx, actor, p, plan, true)
		must(e)
		exec(`UPDATE catalog_editor_publications SET status='created',progress='{}' WHERE variant_id=$1 AND channel='wb'`, v)
		var id int64
		var fingerprint, origin string
		must(pool.QueryRow(ctx, `SELECT id,fingerprint,price_origin FROM catalog_editor_price_publications WHERE variant_id=$1 AND channel='wb' AND status='draft' ORDER BY id DESC LIMIT 1`, v).Scan(&id, &fingerprint, &origin))
		if origin != "formula" {
			t.Fatal("automatic save lost formula origin")
		}
		changed := 0.25
		if strike < 0.5 {
			changed = 0.75
		}
		exec(`UPDATE procurement_pricing_settings SET marketplace_strike_markup=$1 WHERE id=1`, changed)
		_, e = repo.ApproveEditorPrice(ctx, actor, p, id, fingerprint)
		if !errors.Is(e, pgx.ErrNoRows) {
			t.Fatalf("changed automatic formula approved: %v", e)
		}
		// A stale automatic form cannot turn its old number into an explicit draft.
		wrong := plan
		wrong.Variants = append([]EditorVariantPlan{}, plan.Variants...)
		wrong.Variants[0].WBPrice = fmt.Sprintf("%.2f", float64(price+100)/100)
		_, e = repo.SaveEditorPlan(ctx, actor, p, wrong, true)
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("stale automatic value accepted: %v", e)
		}
		plan.Variants[0].PriceMode = "manual"
		plan.Variants[0].WBPrice = "2540.25"
		_, e = repo.SaveEditorPlan(ctx, actor, p, plan, true)
		must(e)
		exec(`UPDATE catalog_editor_publications SET status='created',progress='{}' WHERE variant_id=$1 AND channel='wb'`, v)
		must(pool.QueryRow(ctx, `SELECT id,fingerprint,price_origin FROM catalog_editor_price_publications WHERE variant_id=$1 AND channel='wb' AND status='draft' ORDER BY id DESC LIMIT 1`, v).Scan(&id, &fingerprint, &origin))
		if origin != "explicit" {
			t.Fatal("manual save lost explicit origin")
		}
		exec(`UPDATE procurement_pricing_settings SET marketplace_strike_markup=$1 WHERE id=1`, strike)
		_, e = repo.ApproveEditorPrice(ctx, actor, p, id, fingerprint)
		must(e)
		exec(`UPDATE catalog_editor_price_publications SET status='synced' WHERE id=$1`, id)
	})
	t.Run("expired native and marketplace send leases recover read-only", func(t *testing.T) {
		for _, stageName := range []string{"price-send", "price-native-send"} {
			p, v, target := fixture()
			draft := stage(p, v, target, 25000, nil, "explicit")
			_, e := repo.ApproveEditorPrice(ctx, actor, p, draft.ID, draft.Fingerprint)
			must(e)
			exec(`UPDATE catalog_editor_price_publications SET status='processing',lease_until=NOW()-INTERVAL '1 second',progress=jsonb_build_object('stage',$2::text,'native',jsonb_build_object('operationId','durable-42')) WHERE id=$1`, draft.ID, stageName)
			exec(`UPDATE product_variants SET base_price_minor=99999 WHERE id=$1`, v)
			called := false
			boundary := editorPublisherFunc(func(_ context.Context, _ string, _ ce.Request, s ce.Progress) (ce.Result, error) {
				called = true
				if s.Stage != "price-verify" || !s.Ambiguous || len(s.Native) == 0 {
					t.Fatal("unknown write replayed or receipt lost")
				}
				return ce.Result{Status: "needs_attention", Progress: s}, nil
			})
			must(ce.PricePublicationStep(ctx, pool, boundary, "mock"))
			if !called {
				t.Fatal("changed site blocked reconciliation")
			}
			rows, e := repo.EditorPrices(ctx, p)
			must(e)
			if !rows[0].CanRetry {
				t.Fatal("read-only reconciliation unavailable after site change")
			}
			repo.WithEditorPublicationMode("live")
			_, e = repo.RetryEditorPrice(ctx, actor, p, draft.ID, draft.Fingerprint)
			if !errors.Is(e, pgx.ErrNoRows) {
				t.Fatalf("receipt promoted between modes: %v", e)
			}
			repo.WithEditorPublicationMode("mock")
			_, e = repo.RetryEditorPrice(ctx, actor, p, draft.ID, draft.Fingerprint)
			must(e)
			called = false
			must(ce.PricePublicationStep(ctx, pool, boundary, "mock"))
			if !called {
				t.Fatal("explicit read-only recheck did not run")
			}
		}
	})
	t.Run("shared Avito listing requires every sibling fresh and the available minimum", func(t *testing.T) {
		p, v, _ := fixture()
		siblingProduct, sibling, _ := fixture()
		tx, e := pool.Begin(ctx)
		must(e)
		defer tx.Rollback(ctx)
		txexec := func(sql string, args ...any) { t.Helper(); _, e := tx.Exec(ctx, sql, args...); must(e) }
		item := "price-test-" + tag
		txexec(`INSERT INTO avito_listings(item_id,status) VALUES($1,'active')`, item)
		txexec(`INSERT INTO avito_listing_products(item_id,product_id) VALUES($1,$2),($1,$3)`, item, p, siblingProduct)
		txexec(`UPDATE products SET saby_fields=ARRAY['stock'] WHERE id=ANY($1::bigint[])`, []int64{p, siblingProduct})
		for _, id := range []int64{v, sibling} {
			sabyID := fmt.Sprint(880000000 + id)
			txexec(`INSERT INTO saby_nomenclature(saby_id,code) VALUES($1,$2)`, sabyID, "price-fixture-"+sabyID)
			txexec(`UPDATE product_variants SET saby_id=$2 WHERE id=$1`, id, sabyID)
			txexec(`INSERT INTO inventory(warehouse_id,variant_id,available_qty,synced_at) SELECT id,$1,5,NOW() FROM warehouses WHERE saby_id='saby-ryazan-main' ON CONFLICT(warehouse_id,variant_id) DO UPDATE SET available_qty=5,reserved_qty=0,synced_at=NOW()`, id)
		}
		txexec(`INSERT INTO procurement_integration_sync_state(channel,resource,status,last_success_at) VALUES('saby','catalog','ok',NOW()) ON CONFLICT(channel,resource) DO UPDATE SET status='ok',last_success_at=NOW()`)
		txexec(`UPDATE product_variants SET base_price_minor=7000 WHERE id=$1`, sibling)
		request := ce.Request{Action: "price", ProductID: p, VariantID: v, PriceMinor: 10000, Price: &ce.PriceRequest{TargetID: item, PriceMinor: 7000}}
		check := func(want bool) {
			t.Helper()
			reason, e := ce.PriceSnapshotCurrent(ctx, tx, request, "avito", "mock", "explicit")
			must(e)
			if (reason == "") != want {
				t.Fatalf("fresh minimum validity=%t: %s", want, reason)
			}
		}
		check(true)
		txexec(`UPDATE inventory SET synced_at=NOW()-INTERVAL '3 hours' WHERE variant_id=$1`, sibling)
		check(false)
		txexec(`UPDATE inventory SET synced_at=NOW() WHERE variant_id=$1`, sibling)
		txexec(`UPDATE product_variants SET base_price_minor=8000 WHERE id=$1`, sibling)
		check(false)
		request.Price.PriceMinor = 8000
		check(true)
		txexec(`UPDATE inventory SET reserved_qty=available_qty WHERE variant_id=$1`, sibling)
		check(false)
		request.Price.PriceMinor = 10000
		check(true)
		txexec(`UPDATE procurement_integration_sync_state SET last_success_at=NOW()-INTERVAL '3 hours' WHERE channel='saby' AND resource='catalog'`)
		check(false)
	})
	t.Run("migration-shaped disabled drafts require fresh owner approval", func(t *testing.T) {
		p, v, wb := fixture()
		ozon := "migration-offer-" + tag
		exec(`INSERT INTO catalog_editor_remote_links(variant_id,channel,execution_mode,remote_id,offer_id) VALUES($1,'ozon','mock',$2,$2)`, v, ozon)
		sabyID := fmt.Sprint(890000000 + v)
		sabyCode := "migration-saby-" + tag
		exec(`INSERT INTO saby_nomenclature(saby_id,code) VALUES($1,$2)`, sabyID, sabyCode)
		exec(`UPDATE product_variants SET saby_id=$2 WHERE id=$1`, v, sabyID)
		t.Cleanup(func() { pool.Exec(ctx, `DELETE FROM saby_nomenclature WHERE saby_id=$1`, sabyID) })
		// The minimal JSON produced by migration212 omits all content fields.
		for _, channel := range []struct{ name, target string }{{"wb", wb}, {"ozon", ozon}, {"saby", sabyCode}} {
			var id int64
			must(pool.QueryRow(ctx, `WITH snapshot AS(SELECT jsonb_build_object('action','price','productId',$1::bigint,'variantId',$2::bigint,'priceMinor',10000,'price',jsonb_build_object('targetId',$4::text,'priceMinor',25000)) || CASE WHEN $3='saby' THEN jsonb_build_object('sabyId',$5::text) ELSE '{}'::jsonb END payload) INSERT INTO catalog_editor_price_publications(product_id,variant_id,channel,target_id,expected_site_price_minor,price_minor,price_origin,payload,fingerprint,legacy_proposal_id) SELECT $1,$2,$3,$4,10000,25000,'explicit',payload,encode(sha256(convert_to(payload::text,'UTF8')),'hex'),-$2*10-CASE $3 WHEN 'wb' THEN 1 WHEN 'ozon' THEN 2 ELSE 3 END FROM snapshot RETURNING id`, p, v, channel.name, channel.target, sabyID).Scan(&id))
			rows, e := repo.EditorPrices(ctx, p)
			must(e)
			var draft EditorPricePublication
			for _, r := range rows {
				if r.ID == id {
					draft = r
				}
			}
			if draft.ExecutionMode != "disabled" || draft.Status != "draft" {
				t.Fatal("migration draft autoactivated")
			}
			approved, e := repo.ApproveEditorPrice(ctx, actor, p, id, draft.Fingerprint)
			must(e)
			if approved.Status != "queued" || approved.ExecutionMode != "mock" {
				t.Fatal("fresh approval did not select current mode")
			}
			exec(`UPDATE catalog_editor_price_publications SET status='synced' WHERE id=$1`, id)
		}
	})
}
