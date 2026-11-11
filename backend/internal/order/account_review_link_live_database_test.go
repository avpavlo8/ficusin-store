package order

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAccountReviewLinkUsesPublishedProductCodeOnLiveDatabase(t *testing.T) {
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
	var customerID, orderID, productID, variantID int64
	var sku string
	var code string
	if err := pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at) VALUES($1,$2,'','CI review link',CURRENT_TIMESTAMP) RETURNING id`, "review-link-"+suffix+"@example.invalid", "+798800"+suffix[len(suffix)-5:]).Scan(&customerID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id=$1`, orderID)
		_, _ = pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, productID)
		_, _ = pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, customerID)
	}()
	if err := pool.QueryRow(ctx, `INSERT INTO products(name,slug,status,product_code) VALUES('Same product name',$1,'published',$2) RETURNING id,product_code::TEXT`, "review-link-"+suffix, 900000000000+time.Now().UnixNano()%100000000000).Scan(&productID, &code); err != nil {
		t.Fatal(err)
	}
	if code == fmt.Sprint(productID) {
		t.Fatal("fixture must have a public code different from its internal ID")
	}
	if err := pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,label,base_price_minor) VALUES($1,'D12',149000) RETURNING id,sku`, productID).Scan(&variantID, &sku); err != nil {
		t.Fatal(err)
	}
	number := "CI-REVIEW-" + suffix
	if err := pool.QueryRow(ctx, `INSERT INTO orders(order_number,customer_id,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,payment_method,payment_status,status) VALUES($1,$2,'CI review link','+79990000001','review@example.invalid','pickup',0,1490,1490,'online','paid','completed') RETURNING id`, number, customerID).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO order_items(order_id,product_id,variant_id,sku,product_name,unit_price,quantity) VALUES($1,$2,$3,$4,'Historical product name',1490,1)`, orderID, productID, variantID, sku); err != nil {
		t.Fatal(err)
	}
	repository := NewPostgresRepository(pool)
	check := func(want string) {
		t.Helper()
		detail, err := repository.DetailForCustomer(ctx, customerID, number)
		if err != nil {
			t.Fatal(err)
		}
		if detail == nil || len(detail.Items) != 1 || detail.Items[0].ProductSlug != want || detail.Items[0].ProductName != "Historical product name" {
			t.Fatalf("unexpected order items: %#v", detail)
		}
	}
	check(code)
	if _, err := pool.Exec(ctx, `UPDATE products SET status='draft' WHERE id=$1`, productID); err != nil {
		t.Fatal(err)
	}
	check("")
	if detail, err := repository.DetailForCustomer(ctx, customerID+1, number); err != nil || detail != nil {
		t.Fatalf("foreign order exposed: %#v, %v", detail, err)
	}
}
