package operations

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
	"time"
)

type fixtureAlertSender struct {
	messages []string
	fail     bool
}

func (s *fixtureAlertSender) SendOperations(_ context.Context, text string) error {
	if s.fail {
		return errors.New("fixture offline")
	}
	s.messages = append(s.messages, text)
	return nil
}
func TestAlertFingerprintIgnoresCountsAndAge(t *testing.T) {
	a, _ := alertMessage(Snapshot{Status: "degraded", Checks: []Check{{Code: "refund_receipt_required", Severity: "warning", Affected: 1}}})
	b, _ := alertMessage(Snapshot{Status: "degraded", Checks: []Check{{Code: "refund_receipt_required", Severity: "warning", Affected: 25}}})
	if a != b {
		t.Fatal("count changes would spam alerts")
	}
}
func TestAlertDeliveryPersistsAndRetriesOnLiveDatabase(t *testing.T) {
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
	// These tests use only the isolated CI database and restore the singleton.
	var previous string
	var notified *time.Time
	savedErr := pool.QueryRow(ctx, `SELECT fingerprint,notified_at FROM operations_alert_state WHERE id=true`).Scan(&previous, &notified)
	_, _ = pool.Exec(ctx, `DELETE FROM operations_alert_state`)
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM operations_alert_state`)
		if savedErr == nil {
			_, _ = pool.Exec(ctx, `INSERT INTO operations_alert_state(id,fingerprint,notified_at)VALUES(true,$1,$2)`, previous, notified)
		}
	}()

	probe := NewProbe(pool)
	sender := &fixtureAlertSender{}
	snapshot := Snapshot{Status: "ok"}
	read := func(context.Context) (Snapshot, error) { return snapshot, nil }
	if err = probe.deliverAlert(ctx, sender, read); err != nil {
		t.Fatal(err)
	}
	if len(sender.messages) != 0 {
		t.Fatal("healthy startup is not silent")
	}
	snapshot = Snapshot{Status: "degraded", Checks: []Check{{Code: "refund_receipt_required", Severity: "warning", Affected: 1}}}
	sender.fail = true
	if err = probe.deliverAlert(ctx, sender, read); err == nil {
		t.Fatal("delivery failure disappeared")
	}
	sender.fail = false
	if err = probe.deliverAlert(ctx, sender, read); err != nil || len(sender.messages) != 1 {
		t.Fatalf("failed alert not retried: %v", err)
	}
	if err = probe.deliverAlert(ctx, sender, read); err != nil || len(sender.messages) != 1 {
		t.Fatal("successful alert repeated")
	}
	snapshot = Snapshot{Status: "ok"}
	if err = probe.deliverAlert(ctx, sender, read); err != nil || len(sender.messages) != 2 {
		t.Fatalf("recovery not delivered: %v", err)
	}
}
