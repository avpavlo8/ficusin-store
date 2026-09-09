package operations

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCancelledOutboxIsNotReportedAsStale(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	probe := NewProbe(pool)
	baseline := affectedForCheck(t, ctx, probe, "stale_outbox")
	recipient := fmt.Sprintf("ci-stale-outbox-%d@example.invalid", time.Now().UnixNano())
	var id int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO outbox(recipient, subject, body, created_at)
		VALUES ($1, 'CI stale outbox', 'CI', CURRENT_TIMESTAMP - INTERVAL '20 minutes')
		RETURNING id
	`, recipient).Scan(&id); err != nil {
		t.Fatalf("seed stale outbox: %v", err)
	}
	defer func() { _, _ = pool.Exec(ctx, "DELETE FROM outbox WHERE id=$1", id) }()

	if got := affectedForCheck(t, ctx, probe, "stale_outbox"); got != baseline+1 {
		t.Fatalf("stale outbox was not detected: baseline=%d got=%d", baseline, got)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE outbox
		SET cancelled_at=CURRENT_TIMESTAMP, cancel_reason='mail_not_configured'
		WHERE id=$1
	`, id); err != nil {
		t.Fatalf("cancel outbox: %v", err)
	}
	if got := affectedForCheck(t, ctx, probe, "stale_outbox"); got != baseline {
		t.Fatalf("cancelled outbox still degrades operations: baseline=%d got=%d", baseline, got)
	}
}

func TestSupersededProcurementFailureIsNotReportedAsCurrent(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	probe := NewProbe(pool)
	baseline := affectedForCheck(t, ctx, probe, "failed_procurement_action")
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	var supplierID, orderID, lineID, failedBatchID, resolvedBatchID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO procurement_suppliers(name, kind, default_currency)
		VALUES ($1, 'domestic', 'RUB') RETURNING id
	`, "CI async health "+suffix).Scan(&supplierID); err != nil {
		t.Fatalf("seed supplier: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM procurement_orders WHERE id=$1", orderID)
		_, _ = pool.Exec(ctx, "DELETE FROM procurement_suppliers WHERE id=$1", supplierID)
	}()
	if err := pool.QueryRow(ctx, `
		INSERT INTO procurement_orders(supplier_id, currency, status)
		VALUES ($1, 'RUB', 'ready_to_receive') RETURNING id
	`, supplierID).Scan(&orderID); err != nil {
		t.Fatalf("seed procurement order: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO procurement_order_lines(procurement_order_id, raw_name, ordered_qty)
		VALUES ($1, 'CI async health line', 1) RETURNING id
	`, orderID).Scan(&lineID); err != nil {
		t.Fatalf("seed procurement line: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO procurement_action_batches(procurement_order_id, kind, status)
		VALUES ($1, 'prices', 'partially_completed') RETURNING id
	`, orderID).Scan(&failedBatchID); err != nil {
		t.Fatalf("seed failed batch: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO procurement_action_items(
			batch_id, procurement_order_line_id, channel, external_article,
			new_value, status, error_message, attempts
		) VALUES ($1, $2, 'wb', 'ci-article', 100, 'failed', 'CI historical failure', 5)
	`, failedBatchID, lineID); err != nil {
		t.Fatalf("seed failed procurement action: %v", err)
	}
	if got := affectedForCheck(t, ctx, probe, "failed_procurement_action"); got != baseline+1 {
		t.Fatalf("current procurement failure was not detected: baseline=%d got=%d", baseline, got)
	}

	// A cancelled batch is an intentionally abandoned external plan. Keep its
	// failed child row for audit, but it must not describe current work or keep
	// production health degraded.
	if _, err := pool.Exec(ctx, `
		UPDATE procurement_action_batches SET status='cancelled' WHERE id=$1
	`, failedBatchID); err != nil {
		t.Fatalf("cancel failed batch: %v", err)
	}
	if got := affectedForCheck(t, ctx, probe, "failed_procurement_action"); got != baseline {
		t.Fatalf("failure from cancelled batch still degrades operations: baseline=%d got=%d", baseline, got)
	}

	// Restore the active failure to prove the separate supersession rule below.
	if _, err := pool.Exec(ctx, `
		UPDATE procurement_action_batches SET status='partially_completed' WHERE id=$1
	`, failedBatchID); err != nil {
		t.Fatalf("restore failed batch: %v", err)
	}
	if got := affectedForCheck(t, ctx, probe, "failed_procurement_action"); got != baseline+1 {
		t.Fatalf("restored unresolved failure was not detected: baseline=%d got=%d", baseline, got)
	}

	if err := pool.QueryRow(ctx, `
		INSERT INTO procurement_action_batches(procurement_order_id, kind, status)
		VALUES ($1, 'prices', 'completed') RETURNING id
	`, orderID).Scan(&resolvedBatchID); err != nil {
		t.Fatalf("seed resolved batch: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO procurement_action_items(
			batch_id, procurement_order_line_id, channel, external_article,
			new_value, status, completed_at
		) VALUES ($1, $2, 'wb', 'ci-article', 100, 'completed', CURRENT_TIMESTAMP)
	`, resolvedBatchID, lineID); err != nil {
		t.Fatalf("seed resolved procurement action: %v", err)
	}
	if got := affectedForCheck(t, ctx, probe, "failed_procurement_action"); got != baseline {
		t.Fatalf("superseded procurement failure still degrades operations: baseline=%d got=%d", baseline, got)
	}
}

func affectedForCheck(t *testing.T, ctx context.Context, probe *Probe, code string) int64 {
	t.Helper()
	snapshot, err := probe.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range snapshot.Checks {
		if check.Code == code {
			return check.Affected
		}
	}
	return 0
}