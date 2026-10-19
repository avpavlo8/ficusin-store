package httpapi

import (
	"context"
	"encoding/json"
	"github.com/avpavlo8/ficusin-store/backend/internal/catalog"
	"net/http"
	"net/http/httptest"
	"testing"
)

type scopedCatalogStub struct{ catalogStub }

func (stub scopedCatalogStub) ListForCollection(_ context.Context, slug string) ([]catalog.Product, error) {
	if slug != "tall" {
		return nil, catalog.ErrNotFound
	}
	return []catalog.Product{{ID: "5", SKU: "tall-sku", HeightCm: func() *float64 { v := 150.0; return &v }()}}, nil
}

func TestScopedCatalogSurvivesRouterCacheWrapper(t *testing.T) {
	repo := scopedCatalogStub{catalogStub{products: []catalog.Product{{ID: "5", SKU: "small-sku"}}}}
	router := NewRouter(discardLogger(), testDependencies(repo, authStub{}))
	for _, check := range []struct {
		url    string
		status int
		sku    string
	}{{"/api/v1/catalog", 200, "small-sku"}, {"/api/v1/catalog?collection=tall", 200, "tall-sku"}, {"/api/v1/catalog", 200, "small-sku"}, {"/api/v1/catalog?collection=missing", 404, ""}, {"/api/v1/catalog?collection=invalid%2Fslug", 400, ""}} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, check.url, nil))
		if response.Code != check.status {
			t.Fatalf("%s: %d %s", check.url, response.Code, response.Body.String())
		}
		if check.sku != "" {
			var body struct {
				Products []catalog.Product `json:"products"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if len(body.Products) != 1 || body.Products[0].SKU != check.sku {
				t.Fatalf("cache scope leaked: %+v", body)
			}
		}
	}
}
