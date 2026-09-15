package procurement

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStage05PartialRequestAllocationOnLiveDatabase(t *testing.T) {
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
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	unique := time.Now().UnixNano()
	sabyID := fmt.Sprintf("stage05-%d", unique)
	if _, err = tx.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,name,balance) VALUES($1,$1,'Stage 05 plant',0)`, sabyID); err != nil {
		t.Fatal(err)
	}
	var supplierID int64
	if err = tx.QueryRow(ctx, `INSERT INTO procurement_suppliers(name,kind,default_currency) VALUES($1,'international','EUR') RETURNING id`, fmt.Sprintf("Stage 05 %d", unique)).Scan(&supplierID); err != nil {
		t.Fatal(err)
	}
	var firstOrder, secondOrder int64
	if err = tx.QueryRow(ctx, `INSERT INTO procurement_orders(supplier_id,source_kind,currency,status) VALUES($1,'recommendation','EUR','ordered') RETURNING id`, supplierID).Scan(&firstOrder); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `INSERT INTO procurement_orders(supplier_id,source_kind,currency,status) VALUES($1,'recommendation','EUR','ordered') RETURNING id`, supplierID).Scan(&secondOrder); err != nil {
		t.Fatal(err)
	}
	var firstLine, secondLine int64
	if err = tx.QueryRow(ctx, `INSERT INTO procurement_order_lines(procurement_order_id,saby_id,raw_name,ordered_qty,match_status) VALUES($1,$2,'Stage 05',1,'confirmed') RETURNING id`, firstOrder, sabyID).Scan(&firstLine); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `INSERT INTO procurement_order_lines(procurement_order_id,saby_id,raw_name,ordered_qty,match_status) VALUES($1,$2,'Stage 05',3,'confirmed') RETURNING id`, secondOrder, sabyID).Scan(&secondLine); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO procurement_requests(kind,saby_id,requested_name,quantity) VALUES('customer_order',$1,'first',2),('customer_order',$1,'second',3)`, sabyID); err != nil {
		t.Fatal(err)
	}
	if err = allocateRequests(ctx, tx, firstLine, sabyID, 1); err != nil {
		t.Fatal(err)
	}
	if err = allocateRequests(ctx, tx, secondLine, sabyID, 3); err != nil {
		t.Fatal(err)
	}
	var active, remaining int
	if err = tx.QueryRow(ctx, `SELECT COALESCE((SELECT SUM(active_quantity) FROM procurement_request_allocations WHERE status='active' AND request_id IN(SELECT id FROM procurement_requests WHERE saby_id=$1)),0)::INTEGER,COALESCE((SELECT SUM(quantity) FROM procurement_requests WHERE saby_id=$1),0)::INTEGER-COALESCE((SELECT SUM(active_quantity) FROM procurement_request_allocations WHERE status='active' AND request_id IN(SELECT id FROM procurement_requests WHERE saby_id=$1)),0)::INTEGER`, sabyID).Scan(&active, &remaining); err != nil {
		t.Fatal(err)
	}
	if active != 4 || remaining != 1 {
		t.Fatalf("active=%d remaining=%d", active, remaining)
	}
	if err = allocateRequests(ctx, tx, secondLine, sabyID, 3); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `SELECT COALESCE(SUM(active_quantity),0)::INTEGER FROM procurement_request_allocations WHERE status='active' AND request_id IN(SELECT id FROM procurement_requests WHERE saby_id=$1)`, sabyID).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 4 {
		t.Fatalf("repeated add allocated %d, want 4", active)
	}
	if err = releaseOrderAllocations(ctx, tx, firstOrder, "test_cancel"); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `SELECT COALESCE(SUM(active_quantity),0)::INTEGER FROM procurement_request_allocations WHERE status='active' AND request_id IN(SELECT id FROM procurement_requests WHERE saby_id=$1)`, sabyID).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 3 {
		t.Fatalf("cancel left %d active, want 3", active)
	}
}
