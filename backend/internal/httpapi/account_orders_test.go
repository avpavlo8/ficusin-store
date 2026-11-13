package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/auth"
	"github.com/avpavlo8/ficusin-store/backend/internal/order"
)

type accountPageStub struct {
	orderStub
	owner                int64
	view                 string
	limit, offset, calls int
}

func (stub *accountPageStub) ListPageForCustomer(_ context.Context, owner int64, view string, limit, offset int) (order.CustomerOrderPage, error) {
	stub.owner, stub.view, stub.limit, stub.offset = owner, view, limit, offset
	stub.calls++
	return order.CustomerOrderPage{Orders: []order.Summary{}, Counts: order.CustomerOrderCounts{Work: 1, Completed: 2, Cancelled: 60}, HasMore: true}, nil
}

func TestAccountOrdersViews(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, query := range []string{"?view=work", "?view=completed&limit=10&offset=20", "?view=cancelled&offset=50"} {
		repository := &accountPageStub{}
		request := httptest.NewRequest(http.MethodGet, "/api/v1/account/orders"+query, nil)
		request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "test"})
		response := httptest.NewRecorder()
		accountOrdersHandler(logger, authStub{user: &auth.User{ID: 42}}, repository).ServeHTTP(response, request)
		if response.Code != 200 || repository.owner != 42 || repository.calls != 1 || !strings.Contains(response.Body.String(), `"cancelled":60`) || !strings.Contains(response.Body.String(), `"hasMore":true`) {
			t.Fatalf("query %s: code=%d stub=%+v body=%s", query, response.Code, repository, response.Body.String())
		}
		if strings.Contains(query, "limit=10") && (repository.limit != 10 || repository.offset != 20) {
			t.Fatalf("wrong paging: %+v", repository)
		}
	}
	for _, query := range []string{"?view=unknown", "?view=work&offset=-1", "?view=work&limit=51", "?view=work&offset=x"} {
		repository := &accountPageStub{}
		request := httptest.NewRequest(http.MethodGet, "/api/v1/account/orders"+query, nil)
		request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "test"})
		response := httptest.NewRecorder()
		accountOrdersHandler(logger, authStub{user: &auth.User{ID: 42}}, repository).ServeHTTP(response, request)
		if response.Code != 400 || repository.calls != 0 {
			t.Fatalf("bad query accepted: %s status=%d", query, response.Code)
		}
	}
	t.Run("legacy response and authentication stay compatible", func(t *testing.T) {
		for _, signedIn := range []bool{false, true} {
			repository := &accountPageStub{}
			request := httptest.NewRequest(http.MethodGet, "/api/v1/account/orders", nil)
			if signedIn {
				request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "test"})
			}
			response := httptest.NewRecorder()
			accountOrdersHandler(logger, authStub{user: &auth.User{ID: 42}}, repository).ServeHTTP(response, request)
			want := 401
			if signedIn {
				want = 200
			}
			if response.Code != want || repository.calls != 0 || strings.Contains(response.Body.String(), `"counts"`) {
				t.Fatalf("legacy/auth regression: %d %s", response.Code, response.Body.String())
			}
		}
	})
}
