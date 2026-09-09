package order

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/mail"
	"github.com/jackc/pgx/v5/pgxpool"
)

type testLetterSender struct {
	configured bool
	sent       int
}

func (sender *testLetterSender) Configured() bool { return sender.configured }

func (sender *testLetterSender) Send(_ context.Context, _ mail.Letter) error {
	sender.sent++
	return nil
}

func TestDisabledMailClosesPendingOutboxWithoutPretendingItWasSent(t *testing.T) {
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

	recipient := fmt.Sprintf("ci-disabled-mail-%d@example.invalid", time.Now().UnixNano())
	var id int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO outbox(recipient, subject, body, created_at)
		VALUES ($1, 'CI disabled mail', 'CI', CURRENT_TIMESTAMP - INTERVAL '20 minutes')
		RETURNING id
	`, recipient).Scan(&id); err != nil {
		t.Fatalf("seed outbox: %v", err)
	}
	defer func() { _, _ = pool.Exec(ctx, "DELETE FROM outbox WHERE id=$1", id) }()

	sender := &testLetterSender{}
	worker := NewLetterWorker(pool, sender, slog.New(slog.NewTextHandler(io.Discard, nil)))
	worker.process(ctx)

	var cancelled bool
	var reason string
	var sent bool
	var attempts int
	if err := pool.QueryRow(ctx, `
		SELECT cancelled_at IS NOT NULL, cancel_reason, sent_at IS NOT NULL, attempts
		FROM outbox WHERE id=$1
	`, id).Scan(&cancelled, &reason, &sent, &attempts); err != nil {
		t.Fatalf("read closed outbox: %v", err)
	}
	if !cancelled || reason != "mail_not_configured" || sent || attempts != 0 {
		t.Fatalf(
			"disabled mail was not represented truthfully: cancelled=%v reason=%q sent=%v attempts=%d",
			cancelled, reason, sent, attempts,
		)
	}

	// Enabling SMTP later must not resurrect a message that was explicitly
	// closed while delivery was disabled.
	sender.configured = true
	worker.process(ctx)
	if sender.sent != 0 {
		t.Fatalf("cancelled letter was sent after mail was enabled: sent=%d", sender.sent)
	}
}
