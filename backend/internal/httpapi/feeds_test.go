package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/catalog"
)

type feedCatalogStub struct {
	offers     []catalog.FeedOffer
	categories []catalog.Category
}

func (stub feedCatalogStub) ListFeedOffers(context.Context) ([]catalog.FeedOffer, error) {
	return stub.offers, nil
}
func (stub feedCatalogStub) ListCategories(context.Context) ([]catalog.Category, error) {
	return stub.categories, nil
}

func feedBody(t *testing.T, path string) string {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	repository := feedCatalogStub{offers: []catalog.FeedOffer{{ProductCode: "5", SKU: "1005", Name: "Фикус", Label: "Высота 40 см", Description: "<p>Живое растение</p>", Price: 1290, Stock: 3, Image: "/images/ficus.webp", CategoryID: 2, Category: "Растения > Фикусы", VariantCount: 2}, {ProductCode: "6", SKU: "1006", Name: "Нет в наличии", Price: 500, Stock: 0, Image: "/images/no.webp", CategoryID: 2, Category: "Растения > Фикусы"}}, categories: []catalog.Category{{ID: 1, Name: "Растения", Slug: "plants"}, {ID: 2, ParentID: func() *int64 { value := int64(1); return &value }(), Name: "Фикусы", Slug: "ficus"}}}
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Host = "ficusin.ru"
	request.Header.Set("X-Forwarded-Proto", "https")
	response := httptest.NewRecorder()
	productFeedHandler(logger, repository).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	return response.Body.String()
}

func TestGoogleFeedExportsExactInStockVariant(t *testing.T) {
	body := feedBody(t, "/feeds/google-products.xml")
	for _, want := range []string{`xmlns:g="http://base.google.com/ns/1.0"`, `<g:id>1005</g:id>`, `<g:item_group_id>5</g:item_group_id>`, `https://ficusin.ru/product/5?sku=1005`, `<g:price>1290.00 RUB</g:price>`, `Живое растение`} {
		if !strings.Contains(body, want) {
			t.Errorf("нет %q в %s", want, body)
		}
	}
	if strings.Contains(body, "1006") || strings.Contains(body, "<p>") {
		t.Errorf("фид содержит недоступный товар или HTML: %s", body)
	}
}

func TestYandexFeedContainsHierarchyAndOffer(t *testing.T) {
	body := feedBody(t, "/feeds/yandex.yml")
	for _, want := range []string{`<yml_catalog date=`, `<category id="2" parentId="1">Фикусы</category>`, `<offer id="1005" available="true">`, `<currencyId>RUB</currencyId>`, `<vendorCode>1005</vendorCode>`} {
		if !strings.Contains(body, want) {
			t.Errorf("нет %q в %s", want, body)
		}
	}
}
