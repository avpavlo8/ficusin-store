package procurement

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCombinedProcurementApprovalScopesPricesAndPhysicalMeasurementsOnLiveDatabase(t *testing.T) {
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
	key := fmt.Sprint(time.Now().UnixNano())
	tx, err := pool.Begin(ctx)
	must(err)
	defer tx.Rollback(ctx)
	var product, variant, supplier, purchase, document, line, actorID int64
	var site, wb, saby int64
	must(tx.QueryRow(ctx, `INSERT INTO customers(email,phone,full_name,consent_at) VALUES($1,$2,'Measurement owner',now()) RETURNING id`, key+"-combined@example.invalid", fmt.Sprintf("+77%09d", time.Now().UnixNano()%1000000000)).Scan(&actorID))
	must(tx.QueryRow(ctx, `INSERT INTO products(name,slug,status,catalog_section) VALUES('Проверяемый фикус',$1,'active','plants') RETURNING id`, "combined-receipt-"+key).Scan(&product))
	must(tx.QueryRow(ctx, `INSERT INTO product_variants(product_id,sku,label,base_price_minor,is_active,marketplace_scope) VALUES($1,$2,'D12',100000,1,'marketplaces') RETURNING id`, product, key).Scan(&variant))
	must(tx.QueryRow(ctx, `INSERT INTO procurement_suppliers(name,kind,default_currency) VALUES($1,'international','EUR') RETURNING id`, "Measurement "+key).Scan(&supplier))
	must(tx.QueryRow(ctx, `INSERT INTO procurement_orders(supplier_id,order_number,source_kind,currency,status,calculated_at,calculation_version)
SELECT $1,$2,'invoice','EUR','ready_to_receive',now(),version FROM procurement_pricing_settings WHERE id=1 RETURNING id`, supplier, key).Scan(&purchase))
	must(tx.QueryRow(ctx, `INSERT INTO procurement_documents(supplier_id,procurement_order_id,file_name,content_type,sha256,object_key,size_bytes,parser_kind,currency) VALUES($1,$2,'fixture.pdf','application/pdf',$3,$4,20,'holland_packing_list','EUR') RETURNING id`, supplier, purchase, fmt.Sprintf("%x", sha256.Sum256([]byte(key))), "fixtures/"+key).Scan(&document))
	must(tx.QueryRow(ctx, `INSERT INTO procurement_order_lines(procurement_order_id,procurement_document_id,canonical_variant_id,raw_name,ordered_qty,invoiced_qty,unit_price,match_status,reconciliation_status,pot_diameter_cm,height_cm,dimensions_reviewed,proposed_retail_rub,baseline_site_price_minor,retail_override_rub,channel_quotes)
VALUES($1,$2,$3,'Фикус',2,2,7,'confirmed','matched',14.5,31.25,TRUE,2000,100000,2000,'{"site":{"priceRub":2000,"contributionRub":1000,"breakEvenRub":300}}') RETURNING id`, purchase, document, variant).Scan(&line))
	for _, v := range []struct{ channel, mode, target string }{{"wb", "mock", key}, {"ozon", "mock", "offer-" + key}} {
		_, err := tx.Exec(ctx, `INSERT INTO catalog_editor_remote_links(variant_id,channel,execution_mode,remote_id,offer_id) VALUES($1,$2,$3,$4,$5)`, variant, v.channel, v.mode, key, "offer-"+key)
		must(err)
		_, err = tx.Exec(ctx, `INSERT INTO catalog_editor_publications(variant_id,channel,execution_mode,fingerprint,status,payload,progress)
VALUES($1,$2,$3,'prior','synced',jsonb_build_object('variantId',$1::bigint,'productId',$4::bigint,'action','update','existingId',$5::text),jsonb_build_object('ids',jsonb_build_object('productId',$6::text,'offerId',$7::text)))`, variant, v.channel, v.mode, product, v.target, key, "offer-"+key)
		must(err)
	}
	for channel, id := range map[string]*int64{"site": &site, "wb": &wb, "saby_price": &saby} {
		must(tx.QueryRow(ctx, `INSERT INTO procurement_action_batches(procurement_order_id,kind,channel,calculation_version,calculated_at,created_by)
SELECT id,'prices',$2,calculation_version,calculated_at,$3 FROM procurement_orders WHERE id=$1 RETURNING id`, purchase, channel, actorID).Scan(id))
	}
	_, err = tx.Exec(ctx, `INSERT INTO procurement_action_items(batch_id,procurement_order_line_id,channel,external_article,new_value)
VALUES($1,$3,'site',$4,2000),($2,$3,'saby_price',$4,0)`, site, saby, line, key)
	must(err)
	must(tx.Commit(ctx))
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM procurement_orders WHERE id=$1`, purchase)
		_, _ = pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, product)
		_, _ = pool.Exec(ctx, `DELETE FROM procurement_suppliers WHERE id=$1`, supplier)
		_, _ = pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, actorID)
	})
	store := NewPostgresStore(pool)
	actor := Actor{CustomerID: actorID, Role: "owner"}
	if _, err := store.ApproveBatch(ctx, actor, wb, map[string]bool{"wb": true}, true); err == nil || !strings.Contains(err.Error(), "Сначала сохраните") {
		t.Fatalf("expected WB size-only approval to require site dimensions, got %v", err)
	}
	if batch, err := store.ApproveBatch(ctx, actor, site, nil, true); err != nil || batch.Status != "completed" || len(batch.Items) != 1 || batch.MeasurementsApplied != 1 || batch.MeasurementsQueued != 0 {
		t.Fatalf("expected atomic site price/measurements approval: %+v %v", batch, err)
	}
	var price int64
	var pot, height float64
	must(pool.QueryRow(ctx, `SELECT base_price_minor,variant_numeric_attribute(id,'pot_diameter_cm')::float8,variant_numeric_attribute(id,'height_cm')::float8 FROM product_variants WHERE id=$1`, variant).Scan(&price, &pot, &height))
	if price != 200000 || pot != 14.5 || height != 31.25 {
		t.Fatalf("site price and dimensions did not apply together: %d / %v / %v", price, pot, height)
	}
	var external int
	must(pool.QueryRow(ctx, `SELECT COUNT(*) FROM catalog_editor_publications WHERE variant_id=$1 AND status='prepared'`, variant).Scan(&external))
	if external != 0 {
		t.Fatalf("site click queued %d external publications", external)
	}
	if batch, err := store.ApproveBatch(ctx, actor, wb, map[string]bool{"wb": true}, true); err != nil || batch.Status != "completed" || len(batch.Items) != 0 || batch.MeasurementsQueued != 1 {
		t.Fatalf("expected WB size-only approval: %+v %v", batch, err)
	}
	var selected string
	must(pool.QueryRow(ctx, `SELECT channel FROM catalog_editor_publications WHERE variant_id=$1 AND status='prepared'`, variant).Scan(&selected))
	if selected != "wb" {
		t.Fatalf("WB button staged %q", selected)
	}
	if batch, err := store.ApproveBatch(ctx, actor, wb, map[string]bool{"wb": true}, true); err != nil || batch.MeasurementsQueued != 1 {
		t.Fatalf("repeat the same approval safely: %+v %v", batch, err)
	}
	if batch, err := store.ApproveBatch(ctx, actor, saby, map[string]bool{"saby_price": true}, true); err != nil || len(batch.Items) != 1 || batch.Items[0].Status != "completed" || !strings.Contains(batch.Items[0].ErrorMessage, "Импорт") {
		t.Fatalf("expected manual Saby file with no price API action: %+v %v", batch, err)
	}
	var ozonBatch, ozonItem int64
	must(pool.QueryRow(ctx, `INSERT INTO procurement_action_batches(procurement_order_id,kind,channel,status,created_by) VALUES($1,'prices','ozon','processing',$2) RETURNING id`, purchase, actorID).Scan(&ozonBatch))
	must(pool.QueryRow(ctx, `INSERT INTO procurement_action_items(batch_id,procurement_order_line_id,channel,external_article,new_value,status,pricing_variant_id)
VALUES($1,$2,'ozon',$3,2500,'queued',$4) RETURNING id`, ozonBatch, line, "offer-"+key, variant).Scan(&ozonItem))
	_, err = pool.Exec(ctx, `UPDATE catalog_editor_publications SET status='queued',payload=payload||'{"procurementMeasurements":true}'::jsonb,progress=progress||'{"stage":"ozon-poll"}'::jsonb WHERE variant_id=$1 AND channel='ozon'`, variant)
	must(err)
	var canClaim bool
	must(pool.QueryRow(ctx, `SELECT `+procurementPriceContentAvailable+` FROM procurement_action_items item WHERE id=$1`, ozonItem).Scan(&canClaim))
	if canClaim {
		t.Fatal("a fresh Ozon price must wait while a measurement import is running")
	}
	_, err = pool.Exec(ctx, `UPDATE procurement_action_items SET external_operation_id='price-resume' WHERE id=$1`, ozonItem)
	must(err)
	must(pool.QueryRow(ctx, `SELECT `+procurementPriceContentAvailable+` FROM procurement_action_items item WHERE id=$1`, ozonItem).Scan(&canClaim))
	if !canClaim {
		t.Fatal("readback of an already-sent Ozon price must remain available")
	}

}
