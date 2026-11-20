package admin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOrderCDEKRetryAndManualTrackingOnLiveDatabase(t *testing.T) {
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
	var id, actorID int64
	number := fmt.Sprintf("CDEK-RETRY-%d", time.Now().UnixNano())
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pool.QueryRow(ctx, `INSERT INTO customers(email,phone,full_name,consent_at)
		VALUES($1,$2,'CDEK test',CURRENT_TIMESTAMP) RETURNING id`, number+"@example.invalid", number).Scan(&actorID))
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM admin_audit_log WHERE actor_customer_id=$1`, actorID)
		_, _ = pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, actorID)
	}()
	must(pool.QueryRow(ctx, `INSERT INTO orders(order_number,customer_name,phone,email,delivery_method,delivery_payee,
		delivery_fee,subtotal,total,status,payment_status,cdek_tariff_code,cdek_uuid,cdek_create_state,cdek_status,cdek_last_error)
		VALUES($1,'Test','+70000000000','','cdek','shop',0,100,100,'assembling','paid',136,'rejected-uuid','registered','INVALID',
		'bad_delivery_point: Недоступен пункт выдачи') RETURNING id`,
		number).Scan(&id))
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id=$1`, id) }()

	repository := NewPostgresRepository(pool).WithShipmentQuotes(&packingQuoteStub{}).
		WithOrderPacking(packingSettingsStub{}, false, false, nil)
	initial, err := repository.OrderCDEK(ctx, id)
	must(err)
	if !initial.CanRetry || !initial.CanSetTracking || initial.Error != "bad_delivery_point: Недоступен пункт выдачи" {
		t.Fatalf("unexpected initial state: %+v", initial)
	}
	owner := Actor{Role: RoleOwner, CustomerID: actorID}
	retry := OrderCDEKInput{Revision: initial.Revision, Action: "retry"}
	queued, err := repository.UpdateOrderCDEK(ctx, owner, id, retry)
	must(err)
	if !queued.Pending || queued.CanRetry || queued.CanSetTracking {
		t.Fatalf("retry did not lock the previous shipment: %+v", queued)
	}
	if _, err = repository.UpdateOrderCDEK(ctx, owner, id, retry); !errors.Is(err, ErrOrderCDEKConflict) {
		t.Fatalf("duplicate retry: %v", err)
	}
	_, err = pool.Exec(ctx, `UPDATE orders SET cdek_create_state='registered' WHERE id=$1`, id)
	must(err)
	manual := OrderCDEKInput{Revision: initial.Revision, Action: "set_tracking", TrackNumber: "  1234567890  "}
	saved, err := repository.UpdateOrderCDEK(ctx, owner, id, manual)
	must(err)
	order, err := repository.OrderForPacking(ctx, id)
	must(err)
	if !saved.ManualTrack || saved.CanRetry || saved.Pending || saved.TrackNumber != "1234567890" ||
		order.TrackNumber != saved.TrackNumber || saved.Error != "" {
		t.Fatalf("manual tracking not reflected in order: %+v, track=%q", saved, order.TrackNumber)
	}
	if _, err = repository.UpdateOrderCDEK(ctx, owner, id, retry); !errors.Is(err, ErrOrderCDEKConflict) {
		t.Fatalf("retry with a manual track: %v", err)
	}
	manual.TrackNumber = "https://example.com/"
	if _, err = repository.UpdateOrderCDEK(ctx, owner, id, manual); !errors.Is(err, ErrOrderCDEKInput) {
		t.Fatalf("unvalidated track: %v", err)
	}
}
