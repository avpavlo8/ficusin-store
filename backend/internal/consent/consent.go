// Package consent keeps evidence that a person agreed to the published
// legal documents. A checkbox in the browser proves nothing after the fact,
// so every agreement is written to the database together with the moment,
// the revision of the documents shown at the time, and enough context to
// tie the record back to a person.
package consent

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

// Version identifies the revision of the legal pages a person saw. Bump it
// whenever the wording of the privacy policy or the offer changes, so old
// records keep pointing at the text that was actually agreed to.
const Version = "2026-08-03"

// Documents lists what the checkbox covers, in the order the wording names
// them.
const Documents = "privacy_policy,offer"

const (
	EventRegistration = "registration"
	EventOrder        = "order"
)

// Event is one recorded agreement. CustomerID is absent for a guest
// checkout; OrderID is absent for a registration.
type Event struct {
	CustomerID *int64
	OrderID    *int64
	Event      string
	Phone      string
	IPAddress  string
	UserAgent  string
}

// Executor is satisfied by both a pgx pool and a transaction, so the
// record can be written inside the same transaction as the thing it
// describes.
type Executor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func Record(ctx context.Context, executor Executor, event Event) error {
	if _, err := executor.Exec(ctx, `
		INSERT INTO consent_events (
			customer_id, order_id, event, documents, document_version,
			phone, ip_address, user_agent
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`,
		event.CustomerID,
		event.OrderID,
		event.Event,
		Documents,
		Version,
		truncate(event.Phone, 32),
		truncate(event.IPAddress, 64),
		truncate(event.UserAgent, 500),
	); err != nil {
		return fmt.Errorf("record consent: %w", err)
	}
	return nil
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
