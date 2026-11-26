package catalogeditor

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOzonMeasurementWaitsForProcurementPriceWithoutAttemptOnLiveDatabase(t *testing.T) {
	dsn := os.Getenv("CRM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("CRM_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
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
	var product, variant, supplier, purchase, line, batch, actor int64
	must(tx.QueryRow(ctx, `INSERT INTO customers(email,phone,full_name,consent_at) VALUES($1,$2,'Price owner',now()) RETURNING id`, key+"-measure-order@example.invalid", fmt.Sprintf("+77%09d", time.Now().UnixNano()%1000000000)).Scan(&actor))
	must(tx.QueryRow(ctx, `INSERT INTO products(name,slug,status,catalog_section) VALUES('Ozon plant',$1,'active','plants') RETURNING id`, "measure-price-"+key).Scan(&product))
	must(tx.QueryRow(ctx, `INSERT INTO product_variants(product_id,sku,label,base_price_minor,is_active) VALUES($1,$2,'D12',100000,1) RETURNING id`, product, key).Scan(&variant))
	must(tx.QueryRow(ctx, `INSERT INTO procurement_suppliers(name,kind,default_currency) VALUES($1,'international','EUR') RETURNING id`, "Price priority "+key).Scan(&supplier))
	must(tx.QueryRow(ctx, `INSERT INTO procurement_orders(supplier_id,order_number,source_kind,currency,status) VALUES($1,$2,'invoice','EUR','ready_to_receive') RETURNING id`, supplier, key).Scan(&purchase))
	must(tx.QueryRow(ctx, `INSERT INTO procurement_order_lines(procurement_order_id,canonical_variant_id,raw_name,ordered_qty,invoiced_qty,unit_price,match_status,reconciliation_status) VALUES($1,$2,'Ozon plant',2,2,7,'confirmed','matched') RETURNING id`, purchase, variant).Scan(&line))
	must(tx.QueryRow(ctx, `INSERT INTO procurement_action_batches(procurement_order_id,kind,channel,status,created_by) VALUES($1,'prices','ozon','processing',$2) RETURNING id`, purchase, actor).Scan(&batch))
	_, err = tx.Exec(ctx, `INSERT INTO procurement_action_items(batch_id,procurement_order_line_id,channel,external_article,new_value,status,pricing_variant_id)
VALUES($1,$2,'ozon',$3,2500,'queued',$4)`, batch, line, "offer-"+key, variant)
	must(err)
	_, err = tx.Exec(ctx, `INSERT INTO catalog_editor_publications(variant_id,channel,execution_mode,fingerprint,status,payload,next_attempt_at)
VALUES($1,'ozon','live','receipt-measurements','prepared',jsonb_build_object('variantId',$1::bigint,'productId',$2::bigint,'procurementMeasurements',TRUE,'existingId',$3::text,'action','update'),'2001-01-01')`, variant, product, "offer-"+key)
	must(err)
	must(tx.Commit(ctx))
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, `DELETE FROM catalog_editor_publications WHERE variant_id=$1`, variant); err != nil {
			t.Error(err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM procurement_orders WHERE id=$1`, purchase); err != nil {
			t.Error(err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, product); err != nil {
			t.Error(err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM procurement_suppliers WHERE id=$1`, supplier); err != nil {
			t.Error(err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, actor); err != nil {
			t.Error(err)
		}
	})
	publisher := &channelRecorder{}
	// Do not compete for a lease with the CRM acceptance app's mock worker.
	must(PublicationStep(ctx, pool, publisher, "live"))
	var attempts int
	var message string
	var delayed bool
	must(pool.QueryRow(ctx, `SELECT attempts,message,next_attempt_at>NOW() FROM catalog_editor_publications WHERE variant_id=$1 AND channel='ozon'`, variant).Scan(&attempts, &message, &delayed))
	if len(publisher.channels) != 0 || attempts != 0 || !delayed || !strings.Contains(message, "подтверждения цены") {
		t.Fatalf("waiting consumed an attempt or touched the publisher: publishes=%d attempts=%d delayed=%t message=%q", len(publisher.channels), attempts, delayed, message)
	}
}
