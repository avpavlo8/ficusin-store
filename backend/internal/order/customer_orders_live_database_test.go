package order

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCustomerOrderViewsOnLiveDatabase(t *testing.T) {
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
	suffix := fmt.Sprint(time.Now().UnixNano())
	prefix := "CI-ORDER-VIEWS-" + suffix
	var customerID int64
	if err := pool.QueryRow(ctx, `INSERT INTO customers(email,phone,full_name,consent_at) VALUES($1,$2,'CI order views',CURRENT_TIMESTAMP) RETURNING id`, prefix+"@example.invalid", "+7"+suffix).Scan(&customerID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM orders WHERE order_number LIKE $1`, prefix+"%")
		_, _ = pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, customerID)
	}()
	seed := func(label, status string, owner any, days int) {
		t.Helper()
		_, err := pool.Exec(ctx, `INSERT INTO orders(order_number,customer_id,customer_name,phone,email,delivery_method,status,created_at,delivery_fee,subtotal,total) VALUES($1,$2,'CI views','+70000000000','','pickup',$3,CURRENT_TIMESTAMP-($4::INTEGER * INTERVAL '1 day'),0,100,100)`, prefix+label, owner, status, days)
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 60; i++ {
		seed(fmt.Sprintf("-cancelled-%02d", i), "cancelled", customerID, i)
	}
	for i, status := range []string{"new", "confirmed", "assembling", "processing", "ready", "shipped"} {
		seed("-"+status, status, customerID, 80+i)
	}
	seed("-completed", "completed", customerID, 1)
	seed("-guest", "new", nil, 0)
	repository := NewPostgresRepository(pool)
	work, err := repository.ListPageForCustomer(ctx, customerID, "work", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if work.Counts != (CustomerOrderCounts{Work: 6, Completed: 1, Cancelled: 60}) || len(work.Orders) != 6 || work.HasMore || work.Orders[0].Status != "new" || work.Orders[5].Status != "shipped" {
		t.Fatalf("wrong complete counts/work ordering: %+v", work)
	}
	first, err := repository.ListPageForCustomer(ctx, customerID, "cancelled", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := repository.ListPageForCustomer(ctx, customerID, "cancelled", 50, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Orders) != 50 || !first.HasMore || len(second.Orders) != 10 || second.HasMore || first.Orders[49].OrderNumber == second.Orders[0].OrderNumber {
		t.Fatalf("wrong paging: first=%+v second=%+v", first, second)
	}
	completed, err := repository.ListPageForCustomer(ctx, customerID, "completed", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(completed.Orders) != 1 || completed.Orders[0].Status != "completed" {
		t.Fatalf("wrong completed view: %+v", completed)
	}
	other, err := repository.ListPageForCustomer(ctx, customerID+100000, "work", 50, 0)
	if err != nil || len(other.Orders) != 0 || other.Counts != (CustomerOrderCounts{}) {
		t.Fatalf("ownership: %+v %v", other, err)
	}
}
