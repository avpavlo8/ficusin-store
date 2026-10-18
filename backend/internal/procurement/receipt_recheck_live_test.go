package procurement

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestReceiptReadOnlyRecheckClosesOrderAndKeepsSourceHistory(t *testing.T) {
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
	u := time.Now().UnixNano()
	nomID := fmt.Sprint(u%1000000000 + 1000000000)
	var actor, supplier, order, line, sourceBatch, sourceItem int64
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at) VALUES($1,$2,'','Receipt recheck',CURRENT_TIMESTAMP) RETURNING id`, fmt.Sprintf("recheck-%d@example.invalid", u), fmt.Sprintf("+79%09d", u%1000000000)).Scan(&actor))
	defer pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, actor)
	must(pool.QueryRow(ctx, `INSERT INTO procurement_suppliers(name,kind,default_currency) VALUES($1,'domestic','RUB') RETURNING id`, fmt.Sprintf("Recheck %d", u)).Scan(&supplier))
	defer pool.Exec(ctx, `DELETE FROM procurement_suppliers WHERE id=$1`, supplier)
	_, err = pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,name,price_minor,balance,images) VALUES($1,$2,'Recheck plant',10000,0,ARRAY[]::TEXT[])`, nomID, "X"+nomID)
	must(err)
	defer pool.Exec(ctx, `DELETE FROM saby_nomenclature WHERE saby_id=$1`, nomID)
	must(pool.QueryRow(ctx, `INSERT INTO procurement_orders(supplier_id,order_number,source_kind,currency,status,created_by) VALUES($1,$2,'invoice','RUB','ready_to_receive',$3) RETURNING id`, supplier, fmt.Sprintf("RECHECK-%d", u), actor).Scan(&order))
	defer pool.Exec(ctx, `DELETE FROM procurement_orders WHERE id=$1`, order)
	must(pool.QueryRow(ctx, `INSERT INTO procurement_order_lines(procurement_order_id,raw_name,ordered_qty,invoiced_qty,unit_price,unit_cost_rub,match_status,reconciliation_status,saby_id) VALUES($1,'Recheck plant',2,2,100,150,'confirmed','matched',$2) RETURNING id`, order, nomID).Scan(&line))
	must(pool.QueryRow(ctx, `INSERT INTO procurement_action_batches(procurement_order_id,kind,status,created_by) VALUES($1,'receipt','cancelled',$2) RETURNING id`, order, actor).Scan(&sourceBatch))
	payload := fmt.Sprintf(`{"orderId":%d,"lines":[{"sabyId":"%s","quantity":2,"unitCost":150}]}`, order, nomID)
	must(pool.QueryRow(ctx, `INSERT INTO procurement_action_items(batch_id,procurement_order_line_id,channel,external_article,new_value,quantity,status,payload,external_operation_id,external_url,attempts,error_message) VALUES($1,$2,'saby_receipt',$3,0,2,'failed',$4,'77','https://ret.saby.ru/doc/11111111-1111-4111-8111-111111111111',5,'Historical readback failure') RETURNING id`, sourceBatch, line, fmt.Sprint(order), payload).Scan(&sourceItem))
	store := NewPostgresStore(pool)
	// A changed quantity must not be reconciled against an older receipt.
	_, err = pool.Exec(ctx, `UPDATE procurement_order_lines SET invoiced_qty=3 WHERE id=$1`, line)
	must(err)
	if _, err = store.RecheckReceipt(ctx, Actor{CustomerID: actor}, sourceBatch); err == nil {
		t.Fatal("changed receipt composition accepted")
	}
	_, err = pool.Exec(ctx, `UPDATE procurement_order_lines SET invoiced_qty=2 WHERE id=$1`, line)
	must(err)
	batch, err := store.RecheckReceipt(ctx, Actor{CustomerID: actor}, sourceBatch)
	must(err)
	if len(batch.Items) != 1 || batch.Items[0].ExternalOperationID != "77" || batch.Items[0].Status != "queued" {
		t.Fatalf("unexpected readback job: %+v", batch)
	}
	again, err := store.RecheckReceipt(ctx, Actor{CustomerID: actor}, sourceBatch)
	must(err)
	if again.ID != batch.ID {
		t.Fatal("double click created another verification")
	}
	item := batch.Items[0]
	var storedPayload json.RawMessage
	must(pool.QueryRow(ctx, `SELECT payload FROM procurement_action_items WHERE id=$1`, item.ID).Scan(&storedPayload))
	var data map[string]any
	must(json.Unmarshal(storedPayload, &data))
	if data["verifyOnly"] != true {
		t.Fatal("read-only marker missing")
	}
	_, err = pool.Exec(ctx, `UPDATE procurement_action_items SET status='processing',attempts=1,lock_owner='recheck-test',lock_token=1,locked_until=CURRENT_TIMESTAMP+INTERVAL '1 minute' WHERE id=$1`, item.ID)
	must(err)
	_, err = pool.Exec(ctx, `UPDATE procurement_order_lines SET invoiced_qty=3 WHERE id=$1`, line)
	must(err)
	_, err = store.FinishAction(ctx, item.ID, "recheck-test", 1, ActionExecution{Completed: true, ExternalOperationID: "77", ExternalURL: item.ExternalURL}, nil)
	must(err)
	var prematureStatus string
	must(pool.QueryRow(ctx, `SELECT status FROM procurement_orders WHERE id=$1`, order).Scan(&prematureStatus))
	if prematureStatus != "ready_to_receive" {
		t.Fatal("composition changed during readback but order closed")
	}
	_, err = pool.Exec(ctx, `UPDATE procurement_order_lines SET invoiced_qty=2 WHERE id=$1`, line)
	must(err)
	_, err = pool.Exec(ctx, `UPDATE procurement_action_items SET status='processing',lock_owner='recheck-test',lock_token=2 WHERE id=$1`, item.ID)
	must(err)
	applied, err := store.FinishAction(ctx, item.ID, "recheck-test", 2, ActionExecution{Completed: true, ExternalOperationID: "77", ExternalURL: item.ExternalURL}, nil)
	must(err)
	if !applied {
		t.Fatal("verified completion not applied")
	}
	var status, oldStatus, oldError string
	var verified bool
	var quantity int
	var cost float64
	must(pool.QueryRow(ctx, `SELECT o.status,l.invoiced_qty,l.unit_cost_rub FROM procurement_orders o JOIN procurement_order_lines l ON l.procurement_order_id=o.id WHERE l.id=$1`, line).Scan(&status, &quantity, &cost))
	if status != "received" || quantity != 2 || cost != 150 {
		t.Fatalf("status=%s quantity=%d cost=%f", status, quantity, cost)
	}
	must(pool.QueryRow(ctx, `SELECT receipt_verified_at IS NOT NULL FROM procurement_action_items WHERE id=$1`, item.ID).Scan(&verified))
	if !verified {
		t.Fatal("posting verification not persisted")
	}
	must(pool.QueryRow(ctx, `SELECT status,error_message FROM procurement_action_items WHERE id=$1`, sourceItem).Scan(&oldStatus, &oldError))
	if oldStatus != "failed" || oldError != "Historical readback failure" {
		t.Fatal("source history overwritten")
	}
}
