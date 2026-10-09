package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
)

type productLinksStub struct {
	adminRepositoryStub
	read bool
}

func (stub *productLinksStub) ListProductLinks(context.Context) ([]admin.ProductLinks, error) {
	stub.read = true
	return []admin.ProductLinks{{
		ID: 1, Name: "Фикус", Slug: "fikus", Status: "published",
		Variants: []admin.VariantLinks{{ID: 2, SKU: "2", Label: "12 см", Channels: map[string]admin.ChannelLink{"saby": {Linked: true, ExternalIDs: []string{"S-2"}}}}},
		Channels: map[string]admin.ChannelLink{"avito": {Linked: true, ExternalIDs: []string{"A-1"}, ListingNames: []string{"Фикус"}}},
	}}, nil
}

func TestProductLinksRouteReturnsCatalogueMappings(t *testing.T) {
	repository := &productLinksStub{}
	router := NewRouter(discardLogger(), adminDependencies(repository, admin.RoleOwner, ""))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, adminRequest("GET", "/api/v1/admin/products/links", ""))
	if response.Code != 200 || !repository.read {
		t.Fatalf("status=%d read=%t body=%s", response.Code, repository.read, response.Body.String())
	}
	var result struct {
		Products []admin.ProductLinks `json:"products"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Products) != 1 || result.Products[0].Channels["avito"].ExternalIDs[0] != "A-1" {
		t.Fatalf("unexpected response: %+v", result)
	}
}

func TestProductLinksRouteRequiresProductRead(t *testing.T) {
	repository := &productLinksStub{}
	router := NewRouter(discardLogger(), adminDependencies(repository, "viewer", ""))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, adminRequest("GET", "/api/v1/admin/products/links", ""))
	if response.Code != 403 || repository.read {
		t.Fatalf("status=%d read=%t", response.Code, repository.read)
	}
}
