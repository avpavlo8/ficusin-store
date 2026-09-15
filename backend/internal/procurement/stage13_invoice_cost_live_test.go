package procurement

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStage13FinalInvoiceCostBecomesCurrentBeforeReceipt(t *testing.T) {
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
	store := NewPostgresStore(pool)
	unique := time.Now().UnixNano()
	sabyID := fmt.Sprintf("stage13-invoice-cost-%d", unique)
	var productID, variantID, actorID, supplierID, orderID int64
	if _, err = pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,name,balance,price_minor)
		VALUES($1,$1,'Stage 13 invoice cost',0,180000)`, sabyID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO products(name,slug,status,catalog_section)
		VALUES('Stage 13 invoice cost',$1,'active','plants') RETURNING id`, fmt.Sprintf("stage-13-invoice-cost-%d", unique)).Scan(&productID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,saby_id,sku,label,base_price_minor,is_active,
		current_unit_cost_rub,current_unit_cost_kind,current_unit_cost_effective_at)
		VALUES($1,$2,$3,'D12',180000,1,900,'estimated','2035-01-01') RETURNING id`, productID, sabyID, fmt.Sprintf("7%017d", unique%100000000000000000)).Scan(&variantID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO procurement_cost_history(canonical_variant_id,saby_id,unit_cost_rub,cost_kind,source,effective_at)
		VALUES($1,$2,900,'estimated','saby_retail_half_initial','2035-01-01')`, variantID, sabyID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at)
		VALUES($1,$2,'','Stage 13 owner',CURRENT_TIMESTAMP) RETURNING id`, fmt.Sprintf("stage13-invoice-%d@example.invalid", unique), fmt.Sprintf("+77%09d", unique%1000000000)).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_suppliers(name,kind,default_currency)
		VALUES($1,'domestic','RUB') RETURNING id`, fmt.Sprintf("Stage 13 invoice supplier %d", unique)).Scan(&supplierID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_orders(supplier_id,order_number,source_kind,currency,status,created_by)
		VALUES($1,$2,'payment_invoice','RUB','review',$3) RETURNING id`, supplierID, fmt.Sprintf("STAGE-13-INVOICE-%d", unique), actorID).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	var documentID int64
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_documents(supplier_id,procurement_order_id,file_name,content_type,
		size_bytes,sha256,content,parser_kind,parser_version,parse_status,arithmetic_status,document_number,
		currency,line_count,unit_count,product_subtotal,document_total,calculated_total,extracted_text,created_by,revision_no)
		VALUES($1,$2,'stage13-invoice.pdf','application/pdf',4,$3,decode('25504446','hex'),'payment_invoice',1,
		'parsed','ok',$4,'RUB',1,20,7000,7000,7000,'Stage 13 synthetic invoice',$5,1) RETURNING id`,
		supplierID, orderID, fmt.Sprintf("%064x", unique), fmt.Sprintf("INV-%d", unique), actorID).Scan(&documentID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO procurement_order_lines(procurement_order_id,procurement_document_id,saby_id,
		canonical_variant_id,raw_name,ordered_qty,invoiced_qty,unit_price,line_total,match_status,reconciliation_status)
		VALUES($1,$2,$3,$4,'Stage 13 invoice cost',20,20,350,7000,'confirmed','matched')`, orderID, documentID, sabyID, variantID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.CalculateOrder(ctx, Actor{CustomerID: actorID, Role: "owner"}, orderID, CalculationInput{}); err != nil {
		t.Fatal(err)
	}
	var cost float64
	var kind, source, status string
	if err = pool.QueryRow(ctx, `SELECT current_unit_cost_rub::DOUBLE PRECISION,current_unit_cost_kind
		FROM product_variants WHERE id=$1`, variantID).Scan(&cost, &kind); err != nil || cost != 350 || kind != "actual" {
		t.Fatalf("current cost=%.2f kind=%s err=%v", cost, kind, err)
	}
	if err = pool.QueryRow(ctx, `SELECT source FROM procurement_cost_history
		WHERE procurement_order_id=$1 AND canonical_variant_id=$2`, orderID, variantID).Scan(&source); err != nil || source != "final_invoice_calculation" {
		t.Fatalf("cost source=%q err=%v", source, err)
	}
	if err = pool.QueryRow(ctx, `SELECT status FROM procurement_orders WHERE id=$1`, orderID).Scan(&status); err != nil || status != "ready_to_receive" {
		t.Fatalf("order status=%q err=%v", status, err)
	}
}
