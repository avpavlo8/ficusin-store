package httpapi

import (
		"context"
		"errors"
		"io"
		"log/slog"
		"net/http"
		"net/http/httptest"
		"strings"
		"testing"
		"time"

		"github.com/avpavlo8/ficusin-store/backend/internal/auth"
		"github.com/avpavlo8/ficusin-store/backend/internal/catalog"
		"github.com/avpavlo8/ficusin-store/backend/internal/order"
	)

type catalogStub struct {
		products []catalog.Product
		err      error
}

type authStub struct{}

func (authStub) RequestCode(context.Context, string) error {
		return nil
}

func (authStub) VerifyCode(
		context.Context,
		string,
		string,
		auth.Registration,
		string,
	) (string, time.Time, error) {
		return "token", time.Now().Add(time.Hour), nil
}

func (authStub) Logout(context.Context, string) error {
		return nil
}

func (authStub) UserByToken(context.Context, string) (*auth.User, error) {
		return nil, nil
}

type orderStub struct{}

func (orderStub) ListForCustomer(context.Context, int64, int) ([]order.Summary, error) {
		return nil, nil
}

func testDependencies(catalog catalogRepository, authentication authService) Dependencies {
		return Dependencies{
					Catalog: catalog,
					Auth:    authentication,
					Orders:  orderStub{},
				}
}

func (stub catalogStub) ListAvailable(context.Context) ([]catalog.Product, error) {
		return stub.products, stub.err
}

func TestHealth(t *testing.T) {
		t.Parallel()

		request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		response := httptest.NewRecorder()
		NewRouter(discardLogger(), testDependencies(catalogStub{}, authStub{})).
			ServeHTTP(response, request)

		if response.Code != http.StatusOK {
					t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
				}
		if body := strings.TrimSpace(response.Body.String()); body != `{"status":"ok"}` {
					t.Fatalf("body = %s", body)
				}
}

func TestCatalog(t *testing.T) {
		t.Parallel()

		repository := catalogStub{
					products: []catalog.Product{{
									ID:   "monstera",
									Name: "Монстера",
								}},
				}
		request := httptest.NewRequest(http.MethodGet, "/api/v1/catalog", nil)
		response := httptest.NewRecorder()
		NewRouter(discardLogger(), testDependencies(repository, authStub{})).
			ServeHTTP(response, request)

		if response.Code != http.StatusOK {
					t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
				}
		if !strings.Contains(response.Body.String(), `"id":"monstera"`) {
					t.Fatalf("unexpected body: %s", response.Body.String())
				}
}

func TestCatalogFailure(t *testing.T) {
		t.Parallel()

		request := httptest.NewRequest(http.MethodGet, "/api/v1/catalog", nil)
		response := httptest.NewRecorder()
		NewRouter(discardLogger(), testDependencies(
					catalogStub{err: errors.New("database unavailable")},
					authStub{},
				)).ServeHTTP(response, request)

		if response.Code != http.StatusServiceUnavailable {
					t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
				}
}

func discardLogger() *slog.Logger {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
}
