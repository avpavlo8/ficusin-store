package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
)

type catalogToolRepositoryStub struct {
	*adminRepositoryStub
	products []admin.Product
	updates  []admin.ProductUpdate
	media    []admin.ProductMedia
}

func newCatalogToolRepositoryStub() *catalogToolRepositoryStub {
	return &catalogToolRepositoryStub{adminRepositoryStub: &adminRepositoryStub{}}
}

func (stub *catalogToolRepositoryStub) ListProducts(context.Context) ([]admin.Product, error) {
	return stub.products, nil
}

func (stub *catalogToolRepositoryStub) UpdateProduct(_ context.Context, _ admin.Actor, _ int64, update admin.ProductUpdate) (admin.Product, error) {
	stub.updates = append(stub.updates, update)
	return admin.Product{ID: 7, Slug: "fikus", Description: valueOrEmpty(update.Description)}, nil
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (stub *catalogToolRepositoryStub) ListProductMedia(context.Context, int64) ([]admin.ProductMedia, error) {
	return stub.media, nil
}

func (stub *catalogToolRepositoryStub) AddUploadedProductMedia(context.Context, admin.Actor, int64, string, string, string) (admin.ProductMedia, error) {
	return admin.ProductMedia{}, nil
}

func (stub *catalogToolRepositoryStub) DeleteProductMedia(context.Context, admin.Actor, int64, int64) error {
	return nil
}

func (stub *catalogToolRepositoryStub) SetPrimaryProductMedia(context.Context, admin.Actor, int64, int64) error {
	return nil
}

func TestProductUpdateRejectsRetiredFirstVariantAliases(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"priceMinor", "variantLabel", "heightCm", "potDiameterCm", "packageLengthCm", "packageWidthCm", "packageHeightCm", "packageWeightGrams", "wholesaleMinQty", "stock", "sabyFields", "externalIds", "image"} {
		for _, role := range []string{admin.RoleOwner, admin.RoleManager} {
			repository := newCatalogToolRepositoryStub()
			request := adminRequest(http.MethodPatch, "/api/v1/admin/products/7", `{"description":"Do not partially save","`+key+`":null}`)
			response := httptest.NewRecorder()
			NewRouter(discardLogger(), adminDependencies(repository, role, "staff@example.com")).ServeHTTP(response, request)
			want := http.StatusBadRequest
			if role == admin.RoleManager && (key == "stock" || key == "sabyFields") {
				want = http.StatusForbidden
			}
			if response.Code != want || len(repository.updates) != 0 {
				t.Fatalf("%s/%s legacy field accepted: %d %v", role, key, response.Code, repository.updates)
			}
		}
	}
}

func TestProductCreateRejectsRetiredAliases(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"heightCm", "potDiameterCm", "packageLengthCm", "packageWidthCm", "packageHeightCm", "packageWeightGrams", "stock", "image", "shortDescription"} {
		repository := newCatalogToolRepositoryStub()
		request := adminRequest(http.MethodPost, "/api/v1/admin/products", `{"name":"New draft","priceMinor":149099,"`+key+`":null}`)
		response := httptest.NewRecorder()
		NewRouter(discardLogger(), adminDependencies(repository, admin.RoleOwner, "owner@example.com")).ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest || len(repository.createdProducts) != 0 {
			t.Fatalf("%s legacy field accepted: %d", key, response.Code)
		}
	}
}

func TestProductMediaListRequiresAdminReadPermission(t *testing.T) {
	t.Parallel()

	repository := newCatalogToolRepositoryStub()
	repository.media = []admin.ProductMedia{{ID: 3, URL: "/photo.jpg", Primary: true}}
	request := adminRequest(http.MethodGet, "/api/v1/admin/products/7/media", "")
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), adminDependencies(repository, admin.RoleManager, "manager@example.com")).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if body := response.Body.String(); body == "" || body == "{}" {
		t.Fatalf("media response is empty: %s", body)
	}
}
