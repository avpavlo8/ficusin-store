package order

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
)

type registrationRetryCarrier struct {
	*stage13CDEK
	status    integration.Shipment
	cancelErr error
	cancelled int
}

func (c *registrationRetryCarrier) FetchOrder(_ context.Context, uuid string) (integration.Shipment, error) {
	if uuid != "cdek-retry-live-uuid" {
		return integration.Shipment{}, integration.ErrCDEKOrderNotFound
	}
	return c.status, nil
}

func (c *registrationRetryCarrier) CancelRejectedOrder(context.Context, string) error {
	c.cancelled++
	return c.cancelErr
}

func TestCDEKRejectedRegistrationRecoveryOnLiveDatabase(t *testing.T) {
	ctx, pool := cdekReviewPool(t)
	number := fmt.Sprintf("CDEK-WORKER-RETRY-%d", time.Now().UnixNano())
	var id int64
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pool.QueryRow(ctx, `INSERT INTO orders(order_number,customer_name,phone,email,delivery_method,delivery_payee,
		delivery_fee,subtotal,total,status,payment_status,cdek_uuid,cdek_create_state,cdek_status,cdek_attempts)
		VALUES($1,'Test','+70000000000','','cdek','shop',0,100,100,'assembling','paid',
		'cdek-retry-live-uuid','registered','INVALID',1) RETURNING id`, number).Scan(&id))
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id=$1`, id) }()
	carrier := &registrationRetryCarrier{
		stage13CDEK: &stage13CDEK{},
		status:      integration.Shipment{Status: "INVALID", RegistrationError: "bad_delivery_point"},
		cancelErr:   integration.ErrCDEKCancellationPending,
	}
	worker := NewShippingWorker(pool, carrier, stage13ShippingSettings{}, nil, quietLogger())
	worker.refreshStatuses(ctx)
	var message, uuid, state, providerNumber string
	read := func() {
		t.Helper()
		must(pool.QueryRow(ctx, `SELECT cdek_last_error,cdek_uuid,cdek_create_state,cdek_order_number FROM orders WHERE id=$1`, id).
			Scan(&message, &uuid, &state, &providerNumber))
	}
	read()
	if message != "bad_delivery_point" {
		t.Fatalf("asynchronous carrier detail was lost: %q", message)
	}
	_, err := pool.Exec(ctx, `UPDATE orders SET cdek_create_state='retry_cancel' WHERE id=$1`, id)
	must(err)
	worker.retryRejectedShipments(ctx)
	read()
	if state != "retry_cancel" || uuid != "cdek-retry-live-uuid" || providerNumber != "" || carrier.cancelled != 1 {
		t.Fatalf("unconfirmed cancellation unlocked retry: %s %s %s", state, uuid, providerNumber)
	}
	worker.retryRejectedShipments(ctx)
	if carrier.cancelled != 1 {
		t.Fatal("pending cancellation was not deferred")
	}
	carrier.cancelErr = nil
	_, err = pool.Exec(ctx, `UPDATE orders SET cdek_next_attempt_at=NULL WHERE id=$1`, id)
	must(err)
	worker.retryRejectedShipments(ctx)
	read()
	if state != "retry_queued" || uuid != "" || providerNumber != number+"-s1-1" || carrier.cancelled != 2 {
		t.Fatalf("confirmed refusal did not queue a fresh registration: %s %s %s", state, uuid, providerNumber)
	}

	_, err = pool.Exec(ctx, `UPDATE orders SET cdek_create_state='manual',cdek_uuid='cdek-retry-live-uuid',
		cdek_track_number='1234567890' WHERE id=$1`, id)
	must(err)
	carrier.status = integration.Shipment{Status: "DELIVERED", TrackNumber: "external"}
	worker.refreshStatuses(ctx)
	var track, orderStatus string
	must(pool.QueryRow(ctx, `SELECT cdek_track_number,status FROM orders WHERE id=$1`, id).Scan(&track, &orderStatus))
	if track != "1234567890" || orderStatus != "assembling" {
		t.Fatalf("old carrier UUID overwrote the manual shipment: %s %s", track, orderStatus)
	}
}
