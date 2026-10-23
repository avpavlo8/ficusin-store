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
	for _, path := range []string{"/", "/chat", "/discounts", "/cart", "/checkout", "/contacts", "/account/reviews", "/favorites", "/offer", "/product/monstera", "/account/orders/0001-15", "/catalog/ficus", "/collections/easy"} {
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
	for _, want := range []string{`rel="canonical" href="https://ficusin.ru/"`, `"@type":"Store"`, `"@type":"WebSite"`, `"logo":"https://ficusin.ru/assets/brand/graphite-v1/wordmark-graphite.png"`, `property="og:image" content="https://ficusin.ru/assets/brand/graphite-v1/og.png"`, `property="og:image:width" content="1200"`, `property="og:image:height" content="630"`} {
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
	for _, want := range []string{"Фикусы — купить", `rel="canonical" href="https://ficusin.ru/catalog/ficus"`, `"@type":"CollectionPage"`, `"@type":"BreadcrumbList"`, `property="og:image" content="https://ficusin.ru/assets/brand/graphite-v1/og.png"`} {
		if !strings.Contains(string(page), want) {
			t.Errorf("нет %q", want)
		}
	}
	if _, found = withLandingMeta(context.Background(), logger, sitemapCatalogStub{}, nil, "https://ficusin.ru", "/catalog/nope", shell); found {
		t.Fatal("несуществующая категория индексируется")
	}
}

func TestCollectionShareImageUsesCoverBeforeBrandFallback(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	shell := []byte(`<html><head><title>shop</title></head></html>`)
	for _, cover := range []string{"/images/collection.jpg", "https://images.example.test/collection.jpg", ""} {
		page, found := withLandingMeta(context.Background(), logger, nil, sitemapCollectionsStub{items: []catalog.Collection{{Slug: "easy", Title: "Неприхотливые", CoverURL: cover}}}, "https://ficusin.ru", "/collections/easy", shell)
		if !found {
			t.Fatal("collection not found")
		}
		want := "https://ficusin.ru/assets/brand/graphite-v1/og.png"
		if cover != "" {
			want = absoluteFeedURL("https://ficusin.ru", cover)
		}
		body := string(page)
		if !strings.Contains(body, `property="og:image" content="`+want+`"`) || strings.Count(body, `property="og:image"`) != 1 {
			t.Errorf("cover %q: missing or duplicate share image: %s", cover, body)
		}
		if cover != "" && strings.Contains(body, brandShareImagePath) {
			t.Errorf("collection cover was mixed with brand fallback: %s", body)
		}
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
	pageBytes, canonicalSlug, err := withProductMeta(
		context.Background(), logger,
		productMetaStub{detail: catalog.ProductDetail{
			Name:     "Монстера Делициоза",
			Latin:    "Monstera deliciosa",
			Images:   []string{"/assets/product-photo-pending-graphite-v1.svg", "https://example.test/monstera.jpg"},
			Variants: []catalog.Variant{{Price: 1290, Stock: 3, Images: []string{"/assets/product-photo-pending-graphite-v1.svg", "/images/monstera-d12.jpg"}}},
			Rating:   4.8, ReviewsCount: 12,
			Passport: catalog.PlantPassport{FAQ: []catalog.FAQItem{{Question: "Когда пересаживать?", Answer: "Весной."}}},
		}},
		"https://ficusin.ru", "monstera", shell,
	)
	if err != nil {
		t.Fatal(err)
	}
	if canonicalSlug == "" {
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
		`property="og:image" content="https://example.test/monstera.jpg"`,
		`"image":"https://ficusin.ru/images/monstera-d12.jpg"`,
		`"@type":"AggregateRating"`,
		`"reviewCount":12`,
		`"@type":"FAQPage"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("в странице нет %q", want)
		}
	}
	if strings.Count(page, `property="og:image"`) != 1 || strings.Contains(page, brandShareImagePath) {
		t.Fatal("product photo was mixed with brand fallback")
	}
}

func TestProductShareImageFallsBackWithoutPretendingLogoIsProductPhoto(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	shell := []byte(`<html><head><title>shop</title></head></html>`)
	for _, images := range [][]string{nil, {" "}, {"/assets/hero-monstera.webp"}, {"/assets/product-photo-pending.svg"}, {"/assets/product-photo-pending-logo-v1.svg"}, {"/assets/product-photo-pending-graphite-v1.svg"}, {"https://ficusin.ru/assets/product-photo-pending-graphite-v1.svg?v=1"}} {
		detail := catalog.ProductDetail{ID: "42", Name: "Монстера", Images: images, Variants: []catalog.Variant{{Price: 1290, Images: images}}}
		page, _, err := withProductMeta(context.Background(), logger, productMetaStub{detail: detail}, "https://ficusin.ru", "42", shell)
		if err != nil {
			t.Fatal(err)
		}
		body := string(page)
		if !strings.Contains(body, `property="og:image" content="https://ficusin.ru/assets/brand/graphite-v1/og.png"`) {
			t.Errorf("images %v: product without photo has no share image: %s", images, body)
		}
		if strings.Contains(body, `"image":`) {
			t.Errorf("images %v: placeholder was put into Product or Offer schema as a photo: %s", images, body)
		}
	}
}

// Устаревшая ссылка должна дать обработчику возможность вернуть настоящий
// HTTP 404, а не индексируемый soft 404.
func TestProductMetaLeavesShellWhenMissing(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	shell := []byte("<html><head><title>Фикусин</title></head></html>")
	page, canonicalSlug, err := withProductMeta(
		context.Background(), logger,
		productMetaStub{err: catalog.ErrNotFound},
		"https://ficusin.ru", "нет-такого", shell,
	)
	if err != nil {
		t.Fatal(err)
	}
	if canonicalSlug != "" {
		t.Fatal("несуществующий товар найден")
	}
	if string(page) != string(shell) {
		t.Fatalf("оболочку изменили: %s", page)
	}
}

func TestProductHTMLIsAvailableWithoutJavaScriptAndEscapesCatalogueText(t *testing.T) {
	detail := catalog.ProductDetail{ID: "33", Name: "Антуриум", CatalogSection: "plants",
		Description: `Описание <script>alert("x")</script>`, CareInstructions: "Общий уход",
		Passport: catalog.PlantPassport{Lighting: "Яркий рассеянный свет", Watering: "Полив после подсыхания", Pests: "Проверяйте листья",
			FAQ: []catalog.FAQItem{{Question: "Как поливать?", Answer: "Без застоя воды"}}},
		ImportantWarnings: []string{"Беречь от животных"}, Reviews: []catalog.Review{{Author: "Покупатель", Text: `Отзыв <img src=x onerror=alert(1)>`}},
	}
	shell := []byte(`<html><head><title>shop</title></head><body><div id="root"></div></body></html>`)
	page, _, err := withProductMeta(context.Background(), slog.Default(), productMetaStub{detail: detail}, "https://ficusin.ru", "33", shell)
	if err != nil {
		t.Fatal(err)
	}
	body := strings.SplitN(string(page), "</head>", 2)[1]
	for _, want := range []string{`data-product-html`, "Антуриум", "Общий уход", "Яркий рассеянный свет", "Полив после подсыхания", "Проверяйте листья", "Как поливать?", "Без застоя воды", "Беречь от животных", "Покупатель", "Отзыв &lt;img", "Описание &lt;script&gt;"} {
		if !strings.Contains(body, want) {
			t.Errorf("initial body is missing %q", want)
		}
	}
	for _, forbidden := range []string{"<script>", "<img", "hidden", "<noscript"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("unsafe or hidden initial body: %q", forbidden)
		}
	}
}

func TestSPAFallbackRedirectsResolvedLegacyProduct(t *testing.T) {
	staticDir := t.TempDir()
	shell := `<html><head><title>Фикусин</title><meta name="description" content="магазин"></head><body></body></html>`
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte(shell), 0o600); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "https://ficusin.ru/product/saby-1107?sku=25", nil)
	response := httptest.NewRecorder()
	spaFallback(slog.Default(), http.NotFoundHandler(), staticDir, nil, nil, productMetaStub{detail: catalog.ProductDetail{ID: "5", Name: "Аглаонема"}}, nil, nil, nil, "https://ficusin.ru").ServeHTTP(response, request)
	if response.Code != http.StatusMovedPermanently || response.Header().Get("Location") != "https://ficusin.ru/product/5?sku=25" {
		t.Fatalf("status=%d location=%q", response.Code, response.Header().Get("Location"))
	}
}

func TestSPAFallbackRedirectsConsolidatedPlantCategory(t *testing.T) {
	staticDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("<html><head></head><body></body></html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	for old, newSlug := range map[string]string{"limetta": "citrus", "venus-flytrap": "carnivorous-plants", "paml": "soil"} {
		request := httptest.NewRequest(http.MethodGet, "https://ficusin.ru/catalog/"+old+"?sort=price", nil)
		response := httptest.NewRecorder()
		spaFallback(slog.Default(), http.NotFoundHandler(), staticDir, nil, nil, nil, nil, nil, nil, "https://ficusin.ru").ServeHTTP(response, request)
		want := "https://ficusin.ru/catalog/" + newSlug + "?sort=price"
		if response.Code != http.StatusMovedPermanently || response.Header().Get("Location") != want {
			t.Errorf("old=%s status=%d location=%q want=%q", old, response.Code, response.Header().Get("Location"), want)
		}
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
	// Счётчик обязан создаваться явно: подхвата заглушки из внешнего файла не
	// происходит, tag.js её не забирает, и именно поэтому отчёты были пустыми.
	for _, want := range []string{"mc.yandex.ru/metrika/tag.js", "id=98765432", "Ya.Metrika2", "counter.hit(", "dataLayer"} {
		if !strings.Contains(loader, want) {
			t.Errorf("в загрузчике нет %q", want)
		}
	}
}

func TestCareArticlesDirectRoutesMetadataAndSitemap(t *testing.T) {
	staticDir := t.TempDir()
	shell := []byte(`<html><head><title>shop</title><meta name="description" content="shop"></head><body><div id="root"></div></body></html>`)
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), shell, 0o600); err != nil {
		t.Fatal(err)
	}
	handler := spaFallback(slog.Default(), http.NotFoundHandler(), staticDir, nil, nil, nil, nil, nil, nil, "https://ficusin.ru")
	sitemap := sitemapBody(t, sitemapCatalogStub{}, nil)
	for _, slug := range []string{"mealybug", "spider-mite", "yellow-leaves", "root-problems"} {
		path := "/care/" + slug
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path+"?plant=33", nil))
		body := response.Body.String()
		if response.Code != http.StatusOK {
			t.Errorf("%s returned %d", path, response.Code)
		}
		if !strings.Contains(body, `rel="canonical" href="https://ficusin.ru`+path+`"`) || !strings.Contains(body, `"@type":"Article"`) || strings.Contains(body, `noindex`) {
			t.Errorf("article metadata missing: %s", body)
		}
		if !strings.Contains(sitemap, "https://ficusin.ru"+path) {
			t.Errorf("article absent from sitemap: %s", path)
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/care/nonexistent", nil))
	if response.Code != http.StatusNotFound {
		t.Errorf("unknown article returned %d", response.Code)
	}
}

func TestProductMetaPreservesLiteralDollarAndEscapesHTML(t *testing.T) {
	shell := []byte(`<html><head><title>shop</title><meta name="description" content="shop"></head></html>`)
	page, _, err := withProductMeta(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)), productMetaStub{detail: catalog.ProductDetail{Name: `Фикус $1 ${name} & <редкий>`, Variants: []catalog.Variant{{Price: 123, Stock: 1}}}}, "https://ficusin.ru", "ficus", shell)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), `<title>Фикус $1 ${name} &amp; &lt;редкий&gt;`) {
		t.Fatalf("corrupted title: %s", page)
	}
}
