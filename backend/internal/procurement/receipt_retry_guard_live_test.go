package procurement

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestFailedSabyReceiptCannotBeRetriedWithoutReconciliation(t *testing.T) {
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
	unique := time.Now().UnixNano()
	var actorID, supplierID, orderID, lineID, receiptBatchID, priceBatchID int64
	if err := pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at)
		VALUES($1,$2,'','Receipt retry guard',CURRENT_TIMESTAMP) RETURNING id`, fmt.Sprintf("receipt-guard-%d@example.invalid", unique), fmt.Sprintf("+79%09d", unique%1000000000)).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, actorID) //nolint:errcheck
	if err := pool.QueryRow(ctx, `INSERT INTO procurement_suppliers(name,kind,default_currency)
		VALUES($1,'domestic','RUB') RETURNING id`, fmt.Sprintf("Receipt guard %d", unique)).Scan(&supplierID); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM procurement_suppliers WHERE id=$1`, supplierID) //nolint:errcheck
	if err := pool.QueryRow(ctx, `INSERT INTO procurement_orders(supplier_id,order_number,source_kind,currency,status,created_by)
		VALUES($1,$2,'payment_invoice','RUB','ready_to_receive',$3) RETURNING id`, supplierID, fmt.Sprintf("GUARD-%d", unique), actorID).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM procurement_orders WHERE id=$1`, orderID) //nolint:errcheck
	if err := pool.QueryRow(ctx, `INSERT INTO procurement_order_lines(procurement_order_id,raw_name,ordered_qty,invoiced_qty,unit_price,match_status,reconciliation_status)
		VALUES($1,'Receipt guard plant',1,1,100,'confirmed','matched') RETURNING id`, orderID).Scan(&lineID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO procurement_action_batches(procurement_order_id,kind,status,created_by)
		VALUES($1,'receipt','failed',$2) RETURNING id`, orderID, actorID).Scan(&receiptBatchID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO procurement_action_items(batch_id,procurement_order_line_id,channel,external_article,new_value,quantity,status,external_operation_id)
		VALUES($1,$2,'saby_receipt',$3,0,1,'failed','351')`, receiptBatchID, lineID, fmt.Sprint(orderID)); err != nil {
		t.Fatal(err)
	}
	store := NewPostgresStore(pool)
	_, err = store.RetryBatch(ctx, Actor{CustomerID: actorID, Role: "owner"}, receiptBatchID, map[string]bool{"saby_receipt": true})
	var userFacing *UserFacingError
	if !errors.As(err, &userFacing) {
		t.Fatalf("failed Saby receipt retry error=%v, want reconciliation message", err)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM procurement_action_items WHERE batch_id=$1`, receiptBatchID).Scan(&status); err != nil || status != "failed" {
		t.Fatalf("receipt retry status=%s err=%v, want failed", status, err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO procurement_action_batches(procurement_order_id,kind,status,created_by)
		VALUES($1,'prices','failed',$2) RETURNING id`, orderID, actorID).Scan(&priceBatchID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO procurement_action_items(batch_id,procurement_order_line_id,channel,external_article,new_value,status)
		VALUES($1,$2,'wb','12345',200,'failed')`, priceBatchID, lineID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RetryBatch(ctx, Actor{CustomerID: actorID, Role: "owner"}, priceBatchID, map[string]bool{"wb": true}); err != nil {
		t.Fatalf("ordinary marketplace retry: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM procurement_action_items WHERE batch_id=$1`, priceBatchID).Scan(&status); err != nil || status != "queued" {
		t.Fatalf("WB retry status=%s err=%v, want queued", status, err)
	}
}
