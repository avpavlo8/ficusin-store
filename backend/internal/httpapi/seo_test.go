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

	"github.com/avpavlo8/ficusin-store/backend/internal/catalog"
)

type sitemapCatalogStub struct {
	products []catalog.Product
	err      error
}

func (stub sitemapCatalogStub) ListAvailable(context.Context) ([]catalog.Product, error) {
	return stub.products, stub.err
}

func TestKnownAppRoutes(t *testing.T) {
	for _, path := range []string{"/", "/favorites", "/offer", "/product/monstera", "/account/orders/0001-15"} {
		if !knownAppRoute(path) {
			t.Errorf("%s — настоящий адрес магазина, а считается выдуманным", path)
		}
	}
	for _, path := range []string{"/nope", "/wp-admin", "/robots.txt.bak", "/product"} {
		if knownAppRoute(path) {
			t.Errorf("%s — выдуманный адрес, а считается настоящим", path)
		}
	}
}

func sitemapBody(t *testing.T, repository sitemapCatalog) string {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	request := httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil)
	request.Host = "ficusin.ru"
	request.Header.Set("X-Forwarded-Proto", "https")
	response := httptest.NewRecorder()
	sitemapHandler(logger, repository).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("ожидали 200, получили %d", response.Code)
	}
	return response.Body.String()
}

func TestSitemapListsProducts(t *testing.T) {
	body := sitemapBody(t, sitemapCatalogStub{products: []catalog.Product{{ID: "monstera-d12"}}})
	for _, want := range []string{"https://ficusin.ru/", "https://ficusin.ru/product/monstera-d12", "urlset"} {
		if !strings.Contains(body, want) {
			t.Errorf("в карте сайта нет %q", want)
		}
	}
}

// Молчащая база не должна оставлять поисковик без карты: статические страницы
// магазина существуют независимо от каталога.
func TestSitemapSurvivesCatalogFailure(t *testing.T) {
	body := sitemapBody(t, sitemapCatalogStub{err: errors.New("база недоступна")})
	if !strings.Contains(body, "https://ficusin.ru/offer") {
		t.Error("статические страницы пропали вместе с каталогом")
	}
}
