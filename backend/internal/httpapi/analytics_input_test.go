package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	commerceanalytics "github.com/avpavlo8/ficusin-store/backend/internal/analytics"
)

type analyticsInputStore struct{ events []commerceanalytics.Event }

func (s *analyticsInputStore) Record(_ context.Context, _ *int64, events []commerceanalytics.Event) error {
	s.events = events
	return nil
}
func (*analyticsInputStore) RecordOrder(context.Context, string, float64, commerceanalytics.Attribution) error {
	panic("public events cannot record an order")
}
func (*analyticsInputStore) Summary(context.Context, int, string) (commerceanalytics.Summary, error) {
	panic("public events cannot read a summary")
}

func TestAnalyticsPublicInputRejectsUnusedOrderNumber(t *testing.T) {
	for _, withOrder := range []bool{false, true} {
		store := &analyticsInputStore{}
		extra := ""
		want := http.StatusNoContent
		if withOrder {
			extra = `,"orderNumber":"client-supplied-order"`
			want = http.StatusBadRequest
		}
		body := `{"events":[{"eventId":"550e8400-e29b-41d4-a716-446655440000","name":"view_item","visitorId":"550e8400-e29b-41d4-a716-446655440001","sessionId":"550e8400-e29b-41d4-a716-446655440002","productCode":"123","quantity":1` + extra + `}]}`
		response := httptest.NewRecorder()
		analyticsEventsHandler(discardLogger(), nil, store).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/analytics/events", strings.NewReader(body)))
		if response.Code != want {
			t.Fatalf("withOrder=%v status=%d body=%s", withOrder, response.Code, response.Body.String())
		}
		if withOrder && len(store.events) != 0 {
			t.Fatal("retired field reached the analytics store")
		}
		if !withOrder && (len(store.events) != 1 || store.events[0].ProductCode != "123") {
			t.Fatal("current public event was not preserved")
		}
	}
}
