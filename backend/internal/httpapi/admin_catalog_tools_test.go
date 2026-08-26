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
	categories []admin.Category
}

func newCatalogToolRepositoryStub() *catalogToolRepositoryStub {
	return &catalogToolRepositoryStub{adminRepositoryStub: &adminRepositoryStub{}}
}

func (stub *catalogToolRepositoryStub) ListProducts(context.Context) ([]admin.Product, error) {
	return stub.products, nil
}

func (stub *catalogToolRepositoryStub) ListCategories(context.Context) ([]admin.Category, error) { return stub.categories, nil }

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

func TestProductUpdateDoesNotReplaceGalleryWhenPrimaryImageIsUnchanged(t *testing.T) {
	t.Parallel()

	repository := newCatalogToolRepositoryStub()
	repository.products = []admin.Product{{ID: 7, Slug: "fikus", Image: "/existing.jpg"}}
	request := adminRequest(http.MethodPatch, "/api/v1/admin/products/7", `{"description":"Новое описание","image":"/existing.jpg"}`)
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), adminDependencies(repository, admin.RoleOwner, "owner@example.com")).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if len(repository.updates) != 1 {
		t.Fatalf("update calls = %d, want 1", len(repository.updates))
	}
	if repository.updates[0].Image != nil {
		t.Fatalf("unchanged image must be removed from update, got %q", *repository.updates[0].Image)
	}
	if repository.updates[0].Description == nil || *repository.updates[0].Description != "Новое описание" {
		t.Fatalf("description was lost: %+v", repository.updates[0].Description)
	}
}

func TestProductUpdateKeepsIntentionalPrimaryImageChange(t *testing.T) {
	t.Parallel()

	repository := newCatalogToolRepositoryStub()
	repository.products = []admin.Product{{ID: 7, Slug: "fikus", Image: "/existing.jpg"}}
	request := adminRequest(http.MethodPatch, "/api/v1/admin/products/7", `{"image":"/new.jpg"}`)
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), adminDependencies(repository, admin.RoleOwner, "owner@example.com")).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if len(repository.updates) != 1 || repository.updates[0].Image == nil || *repository.updates[0].Image != "/new.jpg" {
		t.Fatalf("intentional image replacement was lost: %+v", repository.updates)
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

func TestPlantToolsUseCategoryRootInsteadOfLegacySection(t *testing.T) {
	repository := newCatalogToolRepositoryStub()
	rootID := int64(1)
	leafID := int64(2)
	repository.categories = []admin.Category{{ID: rootID, Slug: "plants"}, {ID: leafID, ParentID: &rootID, Slug: "ficus"}}
	plant, err := adminProductIsPlant(context.Background(), repository, admin.Product{CategoryID: &leafID, CatalogSection: "pots"})
	if err != nil { t.Fatal(err) }
	if !plant { t.Fatal("plant category must win over stale catalog_section") }
	potID := int64(3)
	repository.categories = append(repository.categories, admin.Category{ID: potID, Slug: "pots"})
	plant, err = adminProductIsPlant(context.Background(), repository, admin.Product{CategoryID: &potID, CatalogSection: "plants"})
	if err != nil { t.Fatal(err) }
	if plant { t.Fatal("non-plant category must not receive plant AI") }
}
