package admin

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCreationRealizationEditGuardPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || parsed.Path != "/ficusin_order_sale_test" || (parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost") {
		t.Fatal("requires loopback dedicated ficusin_order_sale_test")
	}
	ctx := context.Background()
	control, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close(ctx)
	schema := fmt.Sprintf("sale_edit_test_%d", time.Now().UnixNano())
	ident := pgx.Identifier{schema}.Sanitize()
	if _, err = control.Exec(ctx, "CREATE SCHEMA "+ident); err != nil {
		t.Fatal(err)
	}
	defer control.Exec(ctx, "DROP SCHEMA "+ident+" CASCADE")
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, `CREATE TABLE orders(id BIGINT PRIMARY KEY,manager_discount_type TEXT,manager_discount_value NUMERIC);INSERT INTO orders VALUES(1,'fixed',0),(2,'fixed',0);
 CREATE TABLE order_items(id BIGINT PRIMARY KEY,order_id BIGINT,sku TEXT,quantity INTEGER);INSERT INTO order_items VALUES(11,1,'13',2),(12,1,'24',1),(21,2,'13',2);
 CREATE TABLE order_sale_intents(order_id BIGINT,event_trigger TEXT);INSERT INTO order_sale_intents VALUES(1,'created'),(2,'handover');`); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name              string
		id                int64
		items             []OrderEditLine
		discount          *OrderDiscount
		rejected, cleared bool
	}{
		{name: "unchanged default form retains item IDs", id: 1, items: []OrderEditLine{{SKU: "24", Quantity: 1}, {SKU: "13", Quantity: 1}, {SKU: "13", Quantity: 1}}, discount: &OrderDiscount{Type: "fixed"}, cleared: true},
		{name: "changed quantity rejected", id: 1, items: []OrderEditLine{{SKU: "13", Quantity: 3}, {SKU: "24", Quantity: 1}}, rejected: true},
		{name: "changed SKU rejected", id: 1, items: []OrderEditLine{{SKU: "14", Quantity: 2}, {SKU: "24", Quantity: 1}}, rejected: true},
		{name: "changed price discount rejected", id: 1, discount: &OrderDiscount{Type: "fixed", Value: 50}, rejected: true},
		{name: "metadata only allowed", id: 1},
		{name: "historical handover order keeps existing edit behavior", id: 2, items: []OrderEditLine{{SKU: "13", Quantity: 3}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			edit := OrderEdit{Discount: tc.discount}
			if tc.items != nil {
				copy := append([]OrderEditLine{}, tc.items...)
				edit.Items = &copy
			}
			err = guardCreationRealizationEdit(ctx, tx, tc.id, &edit)
			if errors.Is(err, ErrInvalidInput) != tc.rejected {
				t.Fatalf("unexpected guard result: %v", err)
			}
			if tc.cleared && edit.Items != nil {
				t.Fatal("unchanged form would recreate frozen order item IDs")
			}
			if tc.id == 2 && edit.Items == nil {
				t.Fatal("historical order edit was suppressed")
			}
			var itemID int64
			if err = tx.QueryRow(ctx, `SELECT id FROM order_items WHERE order_id=1 AND sku='13'`).Scan(&itemID); err != nil || itemID != 11 {
				t.Fatal("frozen item identity changed", itemID, err)
			}
		})
	}
}
