package order

import (
	"context"
	"log/slog"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/mail"
	"github.com/jackc/pgx/v5/pgxpool"
)

// letterSender is the slice of the mail package this worker needs.
type letterSender interface {
	Configured() bool
	Send(ctx context.Context, letter mail.Letter) error
}

// LetterWorker writes to customers about their orders.
//
// It works from a queue in the database rather than sending inline: a slow
// or unreachable mail server must never hold up an order or a status change.
type LetterWorker struct {
	pool     *pgxpool.Pool
	sender   letterSender
	logger   *slog.Logger
	interval time.Duration
}

func NewLetterWorker(pool *pgxpool.Pool, sender letterSender, logger *slog.Logger) *LetterWorker {
	return &LetterWorker{pool: pool, sender: sender, logger: logger, interval: time.Minute}
}

func (worker *LetterWorker) Run(ctx context.Context) {
	if !worker.sender.Configured() {
		return
	}
	ticker := time.NewTicker(worker.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			worker.process(ctx)
		}
	}
}

func (worker *LetterWorker) process(ctx context.Context) {
	rows, err := worker.pool.Query(ctx, `
		SELECT id, recipient, subject, body
		FROM outbox
		WHERE sent_at IS NULL AND attempts < 5
		ORDER BY id
		LIMIT 20
	`)
	if err != nil {
		worker.logger.Error("read outbox failed", "error", err)
		return
	}
	type pending struct {
		id                        int64
		recipient, subject, body  string
	}
	letters := make([]pending, 0)
	for rows.Next() {
		var item pending
		if err := rows.Scan(&item.id, &item.recipient, &item.subject, &item.body); err != nil {
			worker.logger.Error("scan outbox failed", "error", err)
			break
		}
		letters = append(letters, item)
	}
	rows.Close()

	for _, item := range letters {
		err := worker.sender.Send(ctx, mail.Letter{
			To: item.recipient, Subject: item.subject, Body: item.body,
		})
		if err != nil {
			// Counted, not dropped: a mail server that is down for ten
			// minutes should not cost the customer their confirmation.
			worker.logger.Error("send letter failed", "error", err, "letter_id", item.id)
			if _, failed := worker.pool.Exec(ctx, `
				UPDATE outbox SET attempts = attempts + 1, last_error = $2
				WHERE id = $1
			`, item.id, err.Error()); failed != nil {
				worker.logger.Error("record letter failure", "error", failed)
			}
			continue
		}
		if _, err := worker.pool.Exec(ctx, `
			UPDATE outbox SET sent_at = CURRENT_TIMESTAMP WHERE id = $1
		`, item.id); err != nil {
			worker.logger.Error("mark letter sent failed", "error", err, "letter_id", item.id)
		}
	}
}

// QueueLetter puts a letter in the outbox. Callers do not wait for it: the
// letter matters, but not enough to fail an order over.
func QueueLetter(ctx context.Context, pool *pgxpool.Pool, recipient string, letter mail.Letter) error {
	if recipient == "" {
		return nil
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO outbox (recipient, subject, body) VALUES ($1, $2, $3)
	`, recipient, letter.Subject, letter.Body)
	return err
}
