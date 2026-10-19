package procurement

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestUnifiedReceivedPublicationPreservesReceiptAndGuardsQueuedPrices(t *testing.T) {
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
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	store := NewPostgresStore(pool)
	settings, err := store.loadSettings(ctx)
	must(err)
	tag := fmt.Sprint(time.Now().UnixNano())
	var actorID, supplier, order, line int64
	products, variants := []int64{}, []int64{}
	keys := []string{}
	defer func() {
		if order > 0 {
			_, _ = pool.Exec(ctx, `DELETE FROM procurement_orders WHERE id=$1`, order)
		}
		if len(products) > 0 {
			_, _ = pool.Exec(ctx, `DELETE FROM products WHERE id=ANY($1)`, products)
		}
		if supplier > 0 {
			_, _ = pool.Exec(ctx, `DELETE FROM procurement_suppliers WHERE id=$1`, supplier)
		}
		if len(keys) > 0 {
			_, _ = pool.Exec(ctx, `DELETE FROM saby_nomenclature WHERE saby_id=ANY($1)`, keys)
		}
		if actorID > 0 {
			_, _ = pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, actorID)
		}
	}()
	must(pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at) VALUES($1,$2,'','Unified publication',now()) RETURNING id`, "unified-publish-"+tag+"@example.invalid", "+75"+tag[len(tag)-9:]).Scan(&actorID))
	actor := Actor{CustomerID: actorID, Role: "owner"}
	must(pool.QueryRow(ctx, `INSERT INTO procurement_suppliers(name,kind,default_currency) VALUES($1,'international','EUR') RETURNING id`, "unified-publish-"+tag).Scan(&supplier))
	codes := []string{"X615770892", "X525558640", "X740063870"}
	for i, code := range codes {
		key := fmt.Sprintf("unified-publish-%s-%d", tag, i)
		keys = append(keys, key)
		_, err = pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,name) VALUES($1,$2,$3)`, key, code, []string{"Calamondin D9", "Lime D9", "Lemon D9"}[i])
		must(err)
		var product, variant int64
		must(pool.QueryRow(ctx, `INSERT INTO products(name,slug,status,catalog_section) VALUES($1,$1,'draft','plants') RETURNING id`, key).Scan(&product))
		products = append(products, product)
		must(pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,label,saby_id,is_active,base_price_minor) VALUES($1,'D9',$2,1,99900) RETURNING id`, product, key).Scan(&variant))
		variants = append(variants, variant)
		_, err = pool.Exec(ctx, `INSERT INTO product_external_ids(product_id,variant_id,provider,id_type,external_id,status,is_primary) VALUES($1,$2,'ozon','offer_id',$3,'active',TRUE)`, product, variant, key)
		must(err)
		for _, dim := range []struct{ code, value string }{{"package_length_cm", "15"}, {"package_width_cm", "15"}, {"package_height_cm", "50"}} {
			_, err = pool.Exec(ctx, `INSERT INTO variant_attribute_values(variant_id,attribute_id,value,source) SELECT $1,id,$3::jsonb,'import' FROM attribute_definitions WHERE code=$2`, variant, dim.code, dim.value)
			must(err)
		}
	}
	must(pool.QueryRow(ctx, `INSERT INTO procurement_orders(supplier_id,order_number,source_kind,currency,status,exchange_rate,delivery_to_moscow_rub,calculated_at,calculation_version) VALUES($1,'8208515','manual','EUR','received',122,200900,now(),$2) RETURNING id`, supplier, settings.Version).Scan(&order))
	// Exactly the production distinction: Russian plan name, Latin invoice name.
	must(pool.QueryRow(ctx, `INSERT INTO procurement_order_lines(procurement_order_id,raw_name,invoice_raw_name,ordered_qty,invoiced_qty,unit_cost_rub,proposed_retail_rub,match_status,reconciliation_status) VALUES($1,'Цитрус микс','Citr Mix',20,20,743.12,1561,'confirmed','matched') RETURNING id`, order).Scan(&line))
	migration, err := os.ReadFile("../../../timeweb/migrations/20261019_zzz_citrus_invoice_pricing_links.sql")
	must(err)
	seed := strings.Split(strings.TrimSpace(string(migration)), "ALTER TABLE")[0]
	_, err = pool.Exec(ctx, seed)
	must(err)
	var parts, qty int
	must(pool.QueryRow(ctx, `SELECT COUNT(*),COALESCE(SUM(quantity),0) FROM procurement_pricing_line_links WHERE order_line_id=$1`, line).Scan(&parts, &qty))
	if parts != 3 || qty != 20 {
		t.Fatalf("invoice label seed failed: %d parts, %d units", parts, qty)
	}
	preview, err := loadUnifiedOrderPreview(ctx, pool, settings, order)
	must(err)
	if preview.PricedUnits != 20 || len(preview.Lines) != 3 {
		t.Fatalf("incomplete composition: %+v", preview)
	}
	for _, p := range preview.Lines {
		want := 6
		if p.VariantID == variants[0] {
			want = 8
		}
		if p.Quantity != want {
			t.Fatalf("wrong composition: %+v", p)
		}
	}
	batch, err := store.PrepareBatch(ctx, actor, order, "prices", []string{"ozon"})
	must(err)
	repeated, err := store.PrepareBatch(ctx, actor, order, "prices", []string{"ozon"})
	must(err)
	if repeated.ID != batch.ID || len(batch.Items) != 3 {
		t.Fatal("preparation is not idempotent")
	}
	for _, item := range batch.Items {
		if item.Status != "draft" || item.CompareAtValue != nil || item.NewValue != 4407 {
			t.Fatalf("not an exact review-only seller price: %+v", item)
		}
	}
	if _, err = store.ApproveBatch(ctx, actor, batch.ID, map[string]bool{}); err == nil {
		t.Fatal("unconfigured channel approved")
	}
	// Packaging can change after preview. Approval must recheck current inputs.
	_, err = pool.Exec(ctx, `UPDATE variant_attribute_values SET value='40'::jsonb WHERE variant_id=$1 AND attribute_id=(SELECT id FROM attribute_definitions WHERE code='package_height_cm')`, variants[0])
	must(err)
	if _, err = store.ApproveBatch(ctx, actor, batch.ID, map[string]bool{"ozon": true}); err == nil {
		t.Fatal("stale box approved")
	}
	_, err = pool.Exec(ctx, `UPDATE variant_attribute_values SET value='50'::jsonb WHERE variant_id=$1 AND attribute_id=(SELECT id FROM attribute_definitions WHERE code='package_height_cm')`, variants[0])
	must(err)
	// Mutating an item cannot bypass the immutable snapshot either.
	_, err = pool.Exec(ctx, `UPDATE procurement_action_items SET new_value=new_value+1 WHERE id=$1`, batch.Items[0].ID)
	must(err)
	if _, err = store.ApproveBatch(ctx, actor, batch.ID, map[string]bool{"ozon": true}); err == nil {
		t.Fatal("altered proposal approved")
	}
	_, err = pool.Exec(ctx, `UPDATE procurement_action_items SET new_value=new_value-1 WHERE id=$1`, batch.Items[0].ID)
	must(err)
	approved, err := store.ApproveBatch(ctx, actor, batch.ID, map[string]bool{"ozon": true})
	must(err)
	if approved.Status != "processing" {
		t.Fatalf("not queued: %+v", approved)
	}
	for _, item := range approved.Items {
		if item.Status != "queued" {
			t.Fatalf("unexpected status: %+v", item)
		}
	}
	current, err := store.GuardClaimedPriceActions(ctx, approved.Items)
	must(err)
	if !current {
		t.Fatal("fresh prices blocked")
	}
	var unchanged bool
	must(pool.QueryRow(ctx, `SELECT o.status='received' AND o.exchange_rate=122 AND o.delivery_to_moscow_rub=200900 AND l.canonical_variant_id IS NULL AND l.unit_cost_rub=743.12 AND l.proposed_retail_rub=1561 AND l.invoiced_qty=20 FROM procurement_orders o JOIN procurement_order_lines l ON l.procurement_order_id=o.id WHERE l.id=$1`, line).Scan(&unchanged))
	if !unchanged {
		t.Fatal("publication changed receipt or acquisition cost")
	}
	var altered int
	must(pool.QueryRow(ctx, `SELECT COUNT(*) FROM product_variants WHERE id=ANY($1) AND (base_price_minor<>99900 OR current_unit_cost_rub IS NOT NULL)`, variants).Scan(&altered))
	if altered != 0 {
		t.Fatal("publication changed site prices or stock cost")
	}
	// The guard runs again immediately before an upload and stops a changed link.
	_, err = pool.Exec(ctx, `UPDATE product_external_ids SET external_id=external_id||'-changed' WHERE variant_id=$1 AND provider='ozon'`, variants[0])
	must(err)
	current, err = store.GuardClaimedPriceActions(ctx, approved.Items)
	must(err)
	if current {
		t.Fatal("changed marketplace link uploaded")
	}
	var queued int
	must(pool.QueryRow(ctx, `SELECT COUNT(*) FROM procurement_action_items WHERE batch_id=$1 AND status IN ('queued','processing')`, batch.ID).Scan(&queued))
	if queued != 0 {
		t.Fatal("stale uploads remained queued")
	}
	// No WB link means an explicit skipped item, never a made-up nmID.
	wb, err := store.PrepareBatch(ctx, actor, order, "prices", []string{"wb"})
	must(err)
	for _, item := range wb.Items {
		if item.ExternalArticle != "" || item.CompareAtValue != nil {
			t.Fatal("invented WB identity or rounded discount")
		}
	}
	skipped, err := store.ApproveBatch(ctx, actor, wb.ID, map[string]bool{"wb": true})
	must(err)
	for _, item := range skipped.Items {
		if item.Status != "skipped" {
			t.Fatal("missing WB card was queued")
		}
	}
}
