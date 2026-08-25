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
	products   []catalog.Product
	categories []catalog.Category
	err        error
}

func (stub sitemapCatalogStub) ListAvailable(context.Context) ([]catalog.Product, error) {
	return stub.products, stub.err
}
func (stub sitemapCatalogStub) ListCategories(context.Context) ([]catalog.Category, error) {
	return stub.categories, stub.err
}

type sitemapCollectionsStub struct {
	items []catalog.Collection
	err   error
}

func (stub sitemapCollectionsStub) ListCollections(context.Context) ([]catalog.Collection, error) {
	return stub.items, stub.err
}

func TestKnownAppRoutes(t *testing.T) {
	for _, path := range []string{"/", "/cart", "/checkout", "/contacts", "/account/reviews", "/favorites", "/offer", "/product/monstera", "/account/orders/0001-15", "/catalog/ficus", "/collections/easy"} {
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
	spaFallback(slog.Default(), http.NotFoundHandler(), staticDir, nil, nil, nil, nil, nil, nil, "https://ficusin.ru").ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("GET /cart status = %d, want 200", response.Code)
	}
	if !strings.Contains(response.Body.String(), `id="root"`) {
		t.Fatalf("GET /cart did not return the SPA shell: %q", response.Body.String())
	}
}

func sitemapBody(t *testing.T, repository sitemapCatalog, collections sitemapCollections) string {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	request := httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil)
	request.Host = "ficusin.ru"
	request.Header.Set("X-Forwarded-Proto", "https")
	response := httptest.NewRecorder()
	sitemapHandler(logger, repository, collections, "https://ficusin.ru").ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("ожидали 200, получили %d", response.Code)
	}
	return response.Body.String()
}

func TestSitemapListsProducts(t *testing.T) {
	categoryID := int64(7)
	body := sitemapBody(t, sitemapCatalogStub{products: []catalog.Product{{ID: "monstera-d12", CategoryID: &categoryID}}, categories: []catalog.Category{{ID: 7, Name: "Фикусы", Slug: "ficus"}}}, sitemapCollectionsStub{items: []catalog.Collection{{Slug: "easy", Title: "Неприхотливые", Count: 3}}})
	for _, want := range []string{"https://ficusin.ru/", "https://ficusin.ru/product/monstera-d12", "https://ficusin.ru/catalog/ficus", "https://ficusin.ru/collections/easy", "urlset"} {
		if !strings.Contains(body, want) {
			t.Errorf("в карте сайта нет %q", want)
		}
	}
}

// Молчащая база не должна оставлять поисковик без карты: статические страницы
// магазина существуют независимо от каталога.
func TestSitemapSurvivesCatalogFailure(t *testing.T) {
	body := sitemapBody(t, sitemapCatalogStub{err: errors.New("база недоступна")}, nil)
	if !strings.Contains(body, "https://ficusin.ru/offer") {
		t.Error("статические страницы пропали вместе с каталогом")
	}
}

func TestLandingMetaUsesRealCategoryAndRejectsUnknownSlug(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	shell := []byte(`<html><head><title>shop</title><meta name="description" content="shop"></head></html>`)
	page, found := withLandingMeta(context.Background(), logger, sitemapCatalogStub{categories: []catalog.Category{{ID: 1, Name: "Фикусы", Slug: "ficus"}}}, nil, "https://ficusin.ru", "/catalog/ficus", shell)
	if !found {
		t.Fatal("категория не найдена")
	}
	for _, want := range []string{"Фикусы — купить", `rel="canonical" href="https://ficusin.ru/catalog/ficus"`, `"@type":"CollectionPage"`, `"@type":"BreadcrumbList"`} {
		if !strings.Contains(string(page), want) {
			t.Errorf("нет %q", want)
		}
	}
	if _, found = withLandingMeta(context.Background(), logger, sitemapCatalogStub{}, nil, "https://ficusin.ru", "/catalog/nope", shell); found {
		t.Fatal("несуществующая категория индексируется")
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
	pageBytes, found, err := withProductMeta(
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
	)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("товар не найден")
	}
	page := string(pageBytes)
	for _, want := range []string{
		"<title>Монстера Делициоза",
		`"@type":"Product"`,
		`"price":"1290.00"`,
		"schema.org/InStock",
		`rel="canonical" href="https://ficusin.ru/product/monstera"`,
		`property="og:url" content="https://ficusin.ru/product/monstera"`,
		`"@type":"AggregateRating"`,
		`"reviewCount":12`,
		`"@type":"FAQPage"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("в странице нет %q", want)
		}
	}
}

// Устаревшая ссылка должна дать обработчику возможность вернуть настоящий
// HTTP 404, а не индексируемый soft 404.
func TestProductMetaLeavesShellWhenMissing(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	shell := []byte("<html><head><title>Фикусин</title></head></html>")
	page, found, err := withProductMeta(
		context.Background(), logger,
		productMetaStub{err: catalog.ErrNotFound},
		"https://ficusin.ru", "нет-такого", shell,
	)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("несуществующий товар найден")
	}
	if string(page) != string(shell) {
		t.Fatalf("оболочку изменили: %s", page)
	}
}

func TestSPAFallbackReturns404AndNoIndexForMissingProduct(t *testing.T) {
	staticDir := t.TempDir()
	shell := `<html><head><title>Фикусин</title><meta name="description" content="магазин"></head><body><div id="root"></div></body></html>`
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte(shell), 0o600); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "https://ficusin.ru/product/missing", nil)
	response := httptest.NewRecorder()
	spaFallback(slog.Default(), http.NotFoundHandler(), staticDir, nil, nil, productMetaStub{err: catalog.ErrNotFound}, nil, nil, nil, "https://ficusin.ru").ServeHTTP(response, request)
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `name="robots" content="noindex,nofollow"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestRouteMetaReplacesMultilineProductionDescription(t *testing.T) {
	shell := []byte(`<html><head><title>shop</title><meta
  name="description"
  content="общая страница"
/></head></html>`)
	page := string(withRouteMeta("https://ficusin.ru", "/contacts", shell))
	if strings.Contains(page, "общая страница") || !strings.Contains(page, "Магазин комнатных растений") {
		t.Fatalf("description не заменён: %s", page)
	}
}

func TestRouteMetaReplacesDescriptionInRealFrontendShell(t *testing.T) {
	shell, err := os.ReadFile("../../../frontend/index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(withRouteMeta("https://ficusin.ru", "/contacts", shell))
	if strings.Contains(page, "Комнатные растения с доставкой по Рязани и России") || !strings.Contains(page, "Магазин комнатных растений") {
		t.Fatalf("production description не заменён: %s", page)
	}
}

func TestCanonicalHostRedirect(t *testing.T) {
	next := http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) { response.WriteHeader(http.StatusNoContent) })
	handler := canonicalHostRedirect("https://ficusin.ru", next)
	request := httptest.NewRequest(http.MethodGet, "https://www.ficusin.ru/catalog?x=1", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusPermanentRedirect || response.Header().Get("Location") != "https://ficusin.ru/catalog?x=1" {
		t.Fatalf("redirect status=%d location=%q", response.Code, response.Header().Get("Location"))
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
		if script := analyticsScript(bad); script != "" {
			t.Errorf("загрузчик собрался на негодном номере %q: %s", bad, script)
		}
	}

	page := string(withAnalytics(shell, " 98765432 "))
	for _, want := range []string{`<script src="/analytics.js" async></script>`, "mc.yandex.ru/watch/98765432", "<noscript>"} {
		if !strings.Contains(page, want) {
			t.Errorf("на странице нет %q", want)
		}
	}
	// Встроенного кода на странице быть не должно: политика безопасности
	// запрещает inline-скрипты, и ровно на этом счётчик молча не работал.
	if strings.Contains(page, "<script>(") || strings.Contains(page, "ym(") {
		t.Error("счётчик снова встроен в страницу — браузер отклонит его по CSP")
	}
	if !strings.Contains(page, "</head>") {
		t.Error("счётчик испортил разметку: пропал </head>")
	}

	loader := analyticsScript(" 98765432 ")
	for _, want := range []string{"mc.yandex.ru/metrika/tag.js", `ym(98765432,"init"`, "dataLayer"} {
		if !strings.Contains(loader, want) {
			t.Errorf("в загрузчике нет %q", want)
		}
	}
}
