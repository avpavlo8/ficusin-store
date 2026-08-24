package analytics

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func validEvent() Event {
	return Event{
		EventID: "550e8400-e29b-41d4-a716-446655440000",
		Name:    "view_item", VisitorID: "550e8400-e29b-41d4-a716-446655440001",
		SessionID: "550e8400-e29b-41d4-a716-446655440002", OccurredAt: time.Now(),
	}
}

func TestNewUUIDIsValid(t *testing.T) {
	for range 20 {
		if value := newUUID(); !validUUID(value) {
			t.Fatalf("invalid generated UUID %q", value)
		}
	}
}

func TestNormalizeRejectsForgedPurchase(t *testing.T) {
	event := validEvent()
	event.Name = "order_created"
	if _, err := normalize(event); err != ErrInvalid {
		t.Fatalf("error=%v, want ErrInvalid", err)
	}
}

func TestNormalizeKeepsUTF8WhenTruncating(t *testing.T) {
	event := validEvent()
	event.PageTitle = strings.Repeat("фикус", 100)
	normalized, err := normalize(event)
	if err != nil {
		t.Fatal(err)
	}
	if !utf8.ValidString(normalized.PageTitle) {
		t.Fatal("truncated title is not UTF-8")
	}
	if got := len([]rune(normalized.PageTitle)); got != 300 {
		t.Fatalf("runes=%d, want 300", got)
	}
}

func TestNormalizeReplacesUnreasonableClientTime(t *testing.T) {
	event := validEvent()
	event.OccurredAt = time.Now().Add(-7 * 24 * time.Hour)
	normalized, err := normalize(event)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(normalized.OccurredAt) > time.Minute {
		t.Fatalf("timestamp was not normalized: %v", normalized.OccurredAt)
	}
}
