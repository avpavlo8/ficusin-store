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

func TestCheckedProcurementMeasurementsUpdateSiteAndQueueLinkedVersion(t *testing.T) {
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
	var product, variant, supplier, purchase, document, line int64
	must(tx.QueryRow(ctx, `INSERT INTO products(name,slug,status,catalog_section) VALUES('Проверяемый фикус',$1,'active','plants') RETURNING id`, "receipt-measurement-"+key).Scan(&product))
	must(tx.QueryRow(ctx, `INSERT INTO product_variants(product_id,sku,label,base_price_minor,is_active) VALUES($1,$2,'28 см · D12',123000,1) RETURNING id`, product, key).Scan(&variant))
	must(tx.QueryRow(ctx, `INSERT INTO procurement_suppliers(name,kind,default_currency) VALUES($1,'international','EUR') RETURNING id`, "Measurement "+key).Scan(&supplier))
	must(tx.QueryRow(ctx, `INSERT INTO procurement_orders(supplier_id,order_number,source_kind,currency,status) VALUES($1,$2,'invoice','EUR','ready_to_receive') RETURNING id`, supplier, key).Scan(&purchase))
	must(tx.QueryRow(ctx, `INSERT INTO procurement_documents(supplier_id,procurement_order_id,file_name,content_type,sha256,object_key,size_bytes,parser_kind,currency) VALUES($1,$2,'fixture.pdf','application/pdf',$3,$4,20,'holland_packing_list','EUR') RETURNING id`, supplier, purchase, fmt.Sprintf("%x", sha256.Sum256([]byte(key))), "fixtures/"+key).Scan(&document))
	must(tx.QueryRow(ctx, `INSERT INTO procurement_order_lines(procurement_order_id,procurement_document_id,canonical_variant_id,raw_name,ordered_qty,invoiced_qty,unit_price,match_status,reconciliation_status,pot_diameter_cm,height_cm) VALUES($1,$2,$3,'Фикус',2,2,7,'confirmed','matched',14.5,31.25) RETURNING id`, purchase, document, variant).Scan(&line))
	_, err = tx.Exec(ctx, `INSERT INTO catalog_editor_remote_links(variant_id,channel,execution_mode,remote_id,offer_id) VALUES($1,'wb','mock',$2,$3)`, variant, key, "article-"+key)
	must(err)
	_, err = tx.Exec(ctx, `INSERT INTO catalog_editor_publications(variant_id,channel,execution_mode,fingerprint,status,payload,progress)
VALUES($1,'wb','mock','prior','synced',jsonb_build_object('variantId',$1::bigint,'productId',$2::bigint,'action','update','existingId',$3::text,'name','Saved vendor copy','attributes',jsonb_build_object('pot_diameter_cm',12,'height_cm',28)),jsonb_build_object('ids',jsonb_build_object('productId',$3::text)))`, variant, product, key)
	must(err)
	if err = checkProcurementReceiptMeasurements(ctx, tx, purchase); err == nil || !strings.Contains(err.Error(), "1 строках") {
		t.Fatalf("expected the unchecked invoice line to block receipt, got %v", err)
	}
	_, err = tx.Exec(ctx, `UPDATE procurement_order_lines SET dimensions_reviewed=TRUE WHERE id=$1`, line)
	must(err)
	must(finalizeProcurementReceipt(ctx, tx, purchase))
	var pot, height float64
	var price int64
	var label string
	must(tx.QueryRow(ctx, `SELECT variant_numeric_attribute(id,'pot_diameter_cm')::float8,variant_numeric_attribute(id,'height_cm')::float8,base_price_minor,label FROM product_variants WHERE id=$1`, variant).Scan(&pot, &height, &price, &label))
	if pot != 14.5 || height != 31.25 || price != 123000 || label != "31.25 см · D14.5" {
		t.Fatalf("site variant got pot=%v height=%v price=%d label=%q", pot, height, price, label)
	}
	var status, action, savedName string
	var measurements bool
	must(tx.QueryRow(ctx, `SELECT status,payload->>'action',payload->>'name',(payload->>'procurementMeasurements')::boolean,(payload->'attributes'->>'pot_diameter_cm')::float8,(payload->'attributes'->>'height_cm')::float8 FROM catalog_editor_publications WHERE variant_id=$1 AND channel='wb'`, variant).Scan(&status, &action, &savedName, &measurements, &pot, &height))
	if status != "prepared" || action != "update" || savedName != "Saved vendor copy" || !measurements || pot != 14.5 || height != 31.25 {
		t.Fatalf("external measurement status=%q action=%q name=%q only=%v pot=%v height=%v", status, action, savedName, measurements, pot, height)
	}
	must(finalizeProcurementReceipt(ctx, tx, purchase))
	var audits int
	must(tx.QueryRow(ctx, `SELECT COUNT(*) FROM admin_audit_log WHERE action='procurement.variant.measurements' AND entity_id=$1`, fmt.Sprint(variant)).Scan(&audits))
	if audits != 1 {
		t.Fatalf("expected one update for repeated receipt finalization, got %d", audits)
	}
}
