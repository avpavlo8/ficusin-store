package procurement

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStage07PostedReceiptAndDatedCostsOnLiveDatabase(t *testing.T) {
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
	sabyID := fmt.Sprintf("stage07-%d", unique)
	if _, err = pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,name,balance,price_minor)
		VALUES($1,$1,'Stage 07 plant',4,80000)`, sabyID); err != nil {
		t.Fatal(err)
	}
	var productID, variantID int64
	if err = pool.QueryRow(ctx, `INSERT INTO products(name,slug,status,catalog_section)
		VALUES('Stage 07 plant',$1,'active','plants') RETURNING id`, fmt.Sprintf("stage-07-%d", unique)).Scan(&productID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,saby_id,sku,label,base_price_minor,is_active)
		VALUES($1,$2,$3,'D12',80000,1) RETURNING id`, productID, sabyID, fmt.Sprintf("8%017d", unique%100000000000000000)).Scan(&variantID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO procurement_cost_history(canonical_variant_id,saby_id,unit_cost_rub,cost_kind,source,effective_at)
		VALUES($1,$2,400,'estimated','saby_retail_half_initial','2035-01-01T00:00:00Z')`, variantID, sabyID); err != nil {
		t.Fatal(err)
	}
	var actorID, supplierID, orderID, lineID, batchID, actionID int64
	if err = pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at)
		VALUES($1,$2,'','Stage 07 owner',CURRENT_TIMESTAMP) RETURNING id`, fmt.Sprintf("stage07-%d@example.invalid", unique), fmt.Sprintf("+78%09d", unique%1000000000)).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_suppliers(name,kind,default_currency)
		VALUES($1,'domestic','RUB') RETURNING id`, fmt.Sprintf("Stage 07 %d", unique)).Scan(&supplierID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_orders(supplier_id,order_number,source_kind,currency,status,created_by)
		VALUES($1,'STAGE-07','payment_invoice','RUB','ready_to_receive',$2) RETURNING id`, supplierID, actorID).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_order_lines(procurement_order_id,saby_id,canonical_variant_id,raw_name,
		ordered_qty,invoiced_qty,unit_price,purchase_unit_rub,unit_cost_rub,match_status,reconciliation_status)
		VALUES($1,$2,$3,'Stage 07 plant',20,20,350,350,350,'confirmed','matched') RETURNING id`, orderID, sabyID, variantID).Scan(&lineID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_action_batches(procurement_order_id,kind,status,created_by)
		VALUES($1,'receipt','processing',$2) RETURNING id`, orderID, actorID).Scan(&batchID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_action_items(batch_id,procurement_order_line_id,channel,
		external_article,new_value,quantity,status,lock_owner,lock_token)
		VALUES($1,$2,'saby_receipt',$3,0,20,'processing','stage07-worker',1) RETURNING id`, batchID, lineID, fmt.Sprint(orderID)).Scan(&actionID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.UpdateOrderStatus(ctx, Actor{CustomerID: actorID}, orderID, OrderStatusUpdate{Status: "received"}); err == nil {
		t.Fatal("unverified Saby receipt must not close the procurement")
	}
	applied, err := store.FinishAction(ctx, actionID, "stage07-worker", 1, ActionExecution{Completed: true, ExternalOperationID: "7707"}, nil)
	if err != nil || !applied {
		t.Fatalf("finish receipt applied=%v err=%v", applied, err)
	}
	if applied, err = store.FinishAction(ctx, actionID, "stage07-worker", 1, ActionExecution{Completed: true}, nil); err != nil || applied {
		t.Fatalf("duplicate finisher applied=%v err=%v", applied, err)
	}
	var cost float64
	var kind string
	var verified bool
	if err = pool.QueryRow(ctx, `SELECT current_unit_cost_rub::DOUBLE PRECISION,current_unit_cost_kind
		FROM product_variants WHERE id=$1`, variantID).Scan(&cost, &kind); err != nil || cost != 350 || kind != "actual" {
		t.Fatalf("current cost=%.2f kind=%s err=%v", cost, kind, err)
	}
	if err = pool.QueryRow(ctx, `SELECT receipt_verified_at IS NOT NULL FROM procurement_action_items WHERE id=$1`, actionID).Scan(&verified); err != nil || !verified {
		t.Fatalf("receipt verified=%v err=%v", verified, err)
	}
	if _, err = store.UpdateOrderStatus(ctx, Actor{CustomerID: actorID}, orderID, OrderStatusUpdate{Status: "received"}); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE procurement_cost_history SET effective_at='2035-01-02T00:00:00Z'
		WHERE procurement_order_id=$1 AND canonical_variant_id=$2`, orderID, variantID); err != nil {
		t.Fatal(err)
	}
	eventTime := time.Date(2035, 1, 1, 12, 0, 0, 0, time.UTC)
	record := SalesRecord{ExternalID: sabyID, SabyID: sabyID, SourceEventID: fmt.Sprintf("stage07-sale-%d", unique),
		SourceLineID: "1", EventType: "sale", EventStatus: "confirmed", Date: eventTime, Units: 1, GrossRUB: 800}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = replaceSalesEvents(ctx, tx, "saby", eventTime, eventTime, []SalesRecord{record}); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT unit_cost_rub_snapshot::DOUBLE PRECISION,cost_quality
		FROM sales_events WHERE channel='saby' AND source_event_id=$1`, record.SourceEventID).Scan(&cost, &kind); err != nil || cost != 400 || kind != "estimated" {
		t.Fatalf("late sale cost=%.2f kind=%s err=%v", cost, kind, err)
	}
}
