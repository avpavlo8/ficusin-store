package order

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/avpavlo8/ficusin-store/backend/internal/operations"
	"github.com/jackc/pgx/v5/pgxpool"
)

type cancellationReviewCDEK struct {
	*stage13CDEK
	lookupErr        error
	status           string
	deletes, lookups int
	pending          bool
}

func (client *cancellationReviewCDEK) FetchOrder(context.Context, string) (integration.Shipment, error) {
	client.lookups++
	return integration.Shipment{Status: client.status}, client.lookupErr
}
func (client *cancellationReviewCDEK) CancelOrder(context.Context, string) error {
	client.deletes++
	if client.pending {
		return integration.ErrCDEKCancellationPending
	}
	return nil
}

func cdekReviewPool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return ctx, pool
}
func seedCancellationReview(t *testing.T, ctx context.Context, pool *pgxpool.Pool, state, status string) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(ctx, `INSERT INTO orders(order_number,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,status,cdek_uuid,cdek_cancel_state,cdek_cancel_attempts,cdek_last_error,cdek_create_state) VALUES($1,'Test','+70000000000','','cdek',0,100,100,$2,$3,$4,8,'old 400','registered') RETURNING id`, fmt.Sprintf("CI-cdek-review-%d", time.Now().UnixNano()), status, fmt.Sprintf("review-uuid-%d", time.Now().UnixNano()), state).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id=$1`, id) })
	return id
}
func TestCDEKManualCancellationRecoveryOnLiveDatabase(t *testing.T) {
	ctx, pool := cdekReviewPool(t)
	for _, scenario := range []struct {
		name, status string
		lookupErr    error
		cleared      bool
	}{
		{name: "removed", status: "REMOVED", cleared: true},
		{name: "not found", lookupErr: integration.ErrCDEKOrderNotFound, cleared: true},
		{name: "active", status: "CREATED"},
		{name: "forbidden", lookupErr: errors.New("v2_entity_forbidden")},
		{name: "network failure", lookupErr: errors.New("timeout")},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			id := seedCancellationReview(t, ctx, pool, "manual_review", "cancelled")
			neighbor := seedCancellationReview(t, ctx, pool, "manual_review", "new")
			client := &cancellationReviewCDEK{status: scenario.status, lookupErr: scenario.lookupErr}
			worker := NewShippingWorker(pool, client, &checkoutShippingSettings{}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
			worker.cancelShipments(ctx)
			var state, message, neighborState string
			var attempts int
			var next bool
			if err := pool.QueryRow(ctx, `SELECT cdek_cancel_state,cdek_last_error,cdek_cancel_attempts,cdek_cancel_next_attempt_at IS NOT NULL FROM orders WHERE id=$1`, id).Scan(&state, &message, &attempts, &next); err != nil {
				t.Fatal(err)
			}
			if client.deletes != 0 || client.lookups != 1 || attempts != 8 {
				t.Fatalf("unexpected mutation: deletes=%d reads=%d attempts=%d", client.deletes, client.lookups, attempts)
			}
			if scenario.cleared {
				if state != "cancelled" || message != "" || next {
					t.Fatalf("not reconciled: %s %s next=%v", state, message, next)
				}
			} else if state != "manual_review" || message == "" || !next {
				t.Fatalf("unconfirmed removal hidden: %s %s", state, message)
			}
			if err := pool.QueryRow(ctx, `SELECT cdek_cancel_state FROM orders WHERE id=$1`, neighbor).Scan(&neighborState); err != nil || neighborState != "manual_review" {
				t.Fatalf("active neighbor changed: %s %v", neighborState, err)
			}
			snapshot, err := operations.NewProbe(pool).Snapshot(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var affected int64
			for _, check := range snapshot.Checks {
				if check.Code == "cdek_manual_review" {
					affected = check.Affected
				}
			}
			want := int64(2)
			if scenario.cleared {
				want = 1
			}
			if affected != want {
				t.Fatalf("manual review count=%d want %d", affected, want)
			}
		})
	}
}
func TestCDEKPendingCancellationDoesNotEscalateOnLiveDatabase(t *testing.T) {
	ctx, pool := cdekReviewPool(t)
	id := seedCancellationReview(t, ctx, pool, "retry", "cancelled")
	if _, err := pool.Exec(ctx, `UPDATE orders SET cdek_cancel_attempts=0 WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	client := &cancellationReviewCDEK{pending: true}
	worker := NewShippingWorker(pool, client, &checkoutShippingSettings{}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	worker.cancelShipments(ctx)
	var state string
	if err := pool.QueryRow(ctx, `SELECT cdek_cancel_state FROM orders WHERE id=$1`, id).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "pending" {
		t.Fatalf("state=%s", state)
	}
}
func TestCDEKTrackingLetterQueuedOnceOnLiveDatabase(t *testing.T) {
	ctx, pool := cdekReviewPool(t)
	var customer int64
	suffix := fmt.Sprint(time.Now().UnixNano())
	if err := pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,last_name,consent_at) VALUES($1,$2,'','Александр','Павловский',CURRENT_TIMESTAMP) RETURNING id`, "ci-tracking-"+suffix+"@example.invalid", "ci-tracking-"+suffix).Scan(&customer); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, customer) })
	id := seedCancellationReview(t, ctx, pool, "", "new")
	var number string
	if err := pool.QueryRow(ctx, `UPDATE orders SET customer_id=$2,customer_name='Павловский Александр',email=$3,cdek_track_number='10339991234' WHERE id=$1 RETURNING order_number`, id, customer, "ci-tracking-"+suffix+"@example.invalid").Scan(&number); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM outbox WHERE subject=$1`, "Трек заказа "+number) }()
	worker := NewShippingWorker(pool, &cancellationReviewCDEK{}, &checkoutShippingSettings{}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	worker.queueTrackingLetters(ctx)
	worker.queueTrackingLetters(ctx)
	var count int
	var body string
	if err := pool.QueryRow(ctx, `SELECT COUNT(*),COALESCE(MAX(body),'') FROM outbox WHERE subject=$1`, "Трек заказа "+number).Scan(&count, &body); err != nil {
		t.Fatal(err)
	}
	if count != 1 || !strings.Contains(body, "Здравствуйте, Александр!") || strings.Contains(body, "Здравствуйте, Павловский") || !strings.Contains(body, "order_id=10339991234") {
		t.Fatalf("count=%d body=%s", count, body)
	}
	cancelled := seedCancellationReview(t, ctx, pool, "cancelled", "cancelled")
	if _, err := pool.Exec(ctx, `UPDATE orders SET email='ci@example.invalid',cdek_track_number='cancelled-track' WHERE id=$1`, cancelled); err != nil {
		t.Fatal(err)
	}
	worker.queueTrackingLetters(ctx)
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM outbox WHERE body LIKE '%cancelled-track%'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("cancelled order generated mail", err)
	}
}
