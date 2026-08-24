package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	for _, path := range []string{"/", "/cart", "/checkout", "/contacts", "/account/reviews", "/favorites", "/offer", "/product/monstera", "/account/orders/0001-15"} {
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

func TestRouteMetaIndexesStorefrontAndHidesCheckout(t *testing.T) {
	shell := []byte(`<html><head><title>shop</title><meta name="description" content="shop"></head></html>`)
	home := string(withRouteMeta("https://ficusin.ru", "/", shell))
	for _, want := range []string{`rel="canonical" href="https://ficusin.ru/"`, `"@type":"Store"`, `"@type":"WebSite"`} {
		if !strings.Contains(home, want) {
			t.Errorf("home meta does not contain %q", want)
		}
	}
	checkout := string(withRouteMeta("https://ficusin.ru", "/checkout", shell))
	if !strings.Contains(checkout, `name="robots" content="noindex,nofollow"`) {
		t.Fatalf("checkout is indexable: %s", checkout)
	}
}

func TestSPAFallbackServesDirectCartURL(t *testing.T) {
	staticDir := t.TempDir()
	const shell = "<!doctype html><title>shop</title><div id=\"root\"></div>"
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte(shell), 0o600); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/cart", nil)
	response := httptest.NewRecorder()
	spaFallback(slog.Default(), http.NotFoundHandler(), staticDir, nil, nil, nil).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("GET /cart status = %d, want 200", response.Code)
	}
	if !strings.Contains(response.Body.String(), `id="root"`) {
		t.Fatalf("GET /cart did not return the SPA shell: %q", response.Body.String())
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

type productMetaStub struct {
	detail catalog.ProductDetail
	err    error
}

func (stub productMetaStub) DetailBySlug(context.Context, string) (catalog.ProductDetail, error) {
	return stub.detail, stub.err
}

func TestProductMetaFillsTitleAndSchema(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	shell := []byte(`<html><head><title>Фикусин</title><meta name="description" content="магазин"></head><body></body></html>`)
	page := string(withProductMeta(
		context.Background(), logger,
		productMetaStub{detail: catalog.ProductDetail{
			Name:     "Монстера Делициоза",
			Latin:    "Monstera deliciosa",
			Images:   []string{"https://example.test/monstera.jpg"},
			Variants: []catalog.Variant{{Price: 1290, Stock: 3}},
			Rating:   4.8, ReviewsCount: 12,
			Passport: catalog.PlantPassport{FAQ: []catalog.FAQItem{{Question: "Когда пересаживать?", Answer: "Весной."}}},
		}},
		"https://ficusin.ru", "monstera", shell,
	))
	for _, want := range []string{
		"<title>Монстера Делициоза",
		`"@type":"Product"`,
		`"price":"1290.00"`,
		"schema.org/InStock",
		`rel="canonical" href="https://ficusin.ru/product/monstera"`,
		`"@type":"AggregateRating"`,
		`"reviewCount":12`,
		`"@type":"FAQPage"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("в странице нет %q", want)
		}
	}
}

// Ненайденный товар — обычное дело: ссылка могла устареть. Страница должна
// открыться с общим заголовком магазина, а не сломаться.
func TestProductMetaLeavesShellWhenMissing(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	shell := []byte("<html><head><title>Фикусин</title></head></html>")
	page := withProductMeta(
		context.Background(), logger,
		productMetaStub{err: catalog.ErrNotFound},
		"https://ficusin.ru", "нет-такого", shell,
	)
	if string(page) != string(shell) {
		t.Fatalf("оболочку изменили: %s", page)
	}
}

func TestProductSlugFromPath(t *testing.T) {
	if got := productSlug("/product/monstera-d12"); got != "monstera-d12" {
		t.Errorf("ожидали monstera-d12, получили %q", got)
	}
	for _, path := range []string{"/", "/product/", "/product/a/b", "/favorites"} {
		if got := productSlug(path); got != "" {
			t.Errorf("%s не карточка товара, а вернулось %q", path, got)
		}
	}
}

// Счётчик — единственный чужой скрипт на страницах магазина, поэтому его
// вставка проверяется отдельно: пустая настройка не должна ничего добавлять,
// а мусор в поле не должен превращаться в исполняемый код.
func TestAnalyticsCounterIsInjectedOnlyWhenConfigured(t *testing.T) {
	shell := []byte("<html><head><title>Фикусин</title></head><body></body></html>")

	for _, bad := range []string{"", "   ", "not-a-number", "123", `12345";alert(1);ym(`} {
		if got := string(withAnalytics(shell, bad)); got != string(shell) {
			t.Errorf("настройка %q не должна менять страницу, получили: %s", bad, got)
		}
	}

	page := string(withAnalytics(shell, " 98765432 "))
	for _, want := range []string{
		"mc.yandex.ru/metrika/tag.js",
		`ym(98765432,"init"`,
		"mc.yandex.ru/watch/98765432",
		"<noscript>",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("на странице нет %q", want)
		}
	}
	if !strings.Contains(page, "</head>") {
		t.Error("счётчик испортил разметку: пропал </head>")
	}
}
