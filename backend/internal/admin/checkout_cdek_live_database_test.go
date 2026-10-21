package admin

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCheckoutCDEKLocksCompositionOnLiveDatabase(t *testing.T) {
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
	var id int64
	if err := pool.QueryRow(ctx, `INSERT INTO orders(order_number,customer_name,phone,email,address,comment,delivery_method,delivery_fee,subtotal,total,cdek_uuid,cdek_create_state) VALUES($1,'CDEK test','+70000000000','cdek-lock@example.invalid','','','cdek',450,1490,1940,'test-uuid','registered') RETURNING id`, fmt.Sprint("CDEK-LOCK-", time.Now().UnixNano())).Scan(&id); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id=$1`, id) }()
	repository := NewPostgresRepository(pool)
	actor := Actor{Role: RoleOwner}
	fee := 500.0
	for _, state := range []string{"registered", "creating", "unknown"} {
		uuid := ""
		if state == "registered" {
			uuid = "test-uuid"
		}
		if _, err := pool.Exec(ctx, `UPDATE orders SET cdek_uuid=$2,cdek_create_state=$3 WHERE id=$1`, id, uuid, state); err != nil {
			t.Fatal(err)
		}
		if _, err := repository.EditOrder(ctx, actor, id, OrderEdit{DeliveryFee: &fee}); err == nil || !strings.Contains(err.Error(), "заявка СДЭК") {
			t.Fatalf("%s allowed changing carrier snapshot: %v", state, err)
		}
		if _, err := repository.CreateShipmentOffer(ctx, actor, id, ShipmentOfferInput{Items: []ShipmentOfferLineInput{{OrderItemID: 1, Quantity: 1}}, PackagingRequired: true}); err == nil || !strings.Contains(err.Error(), "заявка СДЭК") {
			t.Fatalf("%s allowed duplicate shipment offer: %v", state, err)
		}
	}
}
