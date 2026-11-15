package operations

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCashierReservationFailureAndOverdueAreVisibleOnLiveDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") || !strings.Contains(u.Path, "test") && u.Path != "/ficusin_ci" {
		t.Fatal("requires isolated loopback test database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	probe := NewProbe(pool)
	tag := fmt.Sprint(time.Now().UnixNano())
	var orderID, warehouseID, reservationID int64
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	baselineReview := affectedForCheck(t, ctx, probe, "cashier_reservation_review")
	baselineOverdue := affectedForCheck(t, ctx, probe, "cashier_reservation_overdue")
	if err = pool.QueryRow(ctx, `INSERT INTO warehouses(saby_id,name,city,address)VALUES($1,'CI native reserve','CI','CI')RETURNING id`, "ci-native-"+tag).Scan(&warehouseID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		pool.Exec(ctx, `DELETE FROM cashier_native_reservations WHERE reservation_id=$1`, reservationID)
		pool.Exec(ctx, `DELETE FROM cashier_stock_reservations WHERE id=$1`, reservationID)
		pool.Exec(ctx, `DELETE FROM orders WHERE id=$1`, orderID)
		pool.Exec(ctx, `DELETE FROM warehouses WHERE id=$1`, warehouseID)
	}()
	if err = pool.QueryRow(ctx, `INSERT INTO orders(order_number,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,payment_method,payment_status,status)
 VALUES($1,'CI native reserve','+70000000000','fixture@example.invalid','pickup',0,100,100,'on_delivery','pending','new')RETURNING id`, "ci-native-"+tag).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO cashier_stock_reservations(order_id,warehouse_id)VALUES($1,$2)RETURNING id`, orderID, warehouseID).Scan(&reservationID); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO cashier_native_reservations(reservation_id)VALUES($1)`, reservationID)
	if got := affectedForCheck(t, ctx, probe, "cashier_reservation_overdue"); got != baselineOverdue {
		t.Fatal("fresh reservation was reported as overdue")
	}
	exec(`UPDATE cashier_native_reservations SET revision_started_at=CURRENT_TIMESTAMP-INTERVAL '20 minutes',updated_at=CURRENT_TIMESTAMP WHERE reservation_id=$1`, reservationID)
	if got := affectedForCheck(t, ctx, probe, "cashier_reservation_overdue"); got != baselineOverdue+1 {
		t.Fatal("fresh retry timestamp hid a delayed revision")
	}
	exec(`UPDATE cashier_native_reservations SET status='needs_attention',last_error='Неизвестен ответ СБИС; повторное создание заблокировано',provider_state='{"identity":{"id":42866,"uuid":"01a1de0d-4726-7e92-98d8-5478ead64c28"}}' WHERE reservation_id=$1`, reservationID)
	if got := affectedForCheck(t, ctx, probe, "cashier_reservation_review"); got != baselineReview+1 {
		t.Fatal("unconfirmed native reserve was hidden")
	}
	diagnostics, err := probe.Diagnose(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range diagnostics.CashierReservations {
		if item.OrderID == orderID {
			found = item.DocumentID == "42866" && item.OrderNumber == "ci-native-"+tag && item.Message != ""
		}
	}
	if !found {
		t.Fatal("native reserve diagnostic lost the order, document or reason")
	}
	exec(`UPDATE cashier_native_reservations SET status='settled',last_error='' WHERE reservation_id=$1`, reservationID)
	if got := affectedForCheck(t, ctx, probe, "cashier_reservation_review"); got != baselineReview {
		t.Fatal("settled reservation still raises a warning")
	}
}
