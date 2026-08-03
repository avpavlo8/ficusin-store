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
	products  []catalog.Product
	detail    catalog.ProductDetail
	err       error
	detailErr error
}

type authStub struct {
	user *auth.User
	err  error
}

func (authStub) RequestCall(context.Context, string) (string, string, string, error) {
	return "check-id", "78000000000", "+7 (800) 000-00-00", nil
}

func (authStub) ConfirmCall(
	context.Context,
	string,
	string,
	auth.Registration,
	string,
) (string, time.Time, bool, error) {
	return "token", time.Now().Add(time.Hour), false, nil
}

func (authStub) UpdateProfile(context.Context, int64, auth.Profile) error {
	return nil
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
func (stub catalogStub) ListCategories(context.Context) ([]catalog.Category,error){ return []catalog.Category{},stub.err }

func (stub catalogStub) DetailBySlug(context.Context, string) (catalog.ProductDetail, error) {
	return stub.detail, stub.detailErr
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

func TestProductDetail(t *testing.T) {
	t.Parallel()

	repository := catalogStub{detail: catalog.ProductDetail{
		ID: "monstera", Name: "Монстера",
	}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/products/monstera", nil)
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), testDependencies(repository, authStub{})).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if !strings.Contains(response.Body.String(), `"id":"monstera"`) {
		t.Fatalf("unexpected body: %s", response.Body.String())
	}
}

func TestProductDetailNotFound(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/api/v1/products/missing", nil)
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), testDependencies(
		catalogStub{detailErr: catalog.ErrNotFound}, authStub{},
	)).ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
