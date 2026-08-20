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
	products       []admin.Product
	updates        []admin.ProductUpdate
	importAllCalls []bool
	media          []admin.ProductMedia
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

func (stub *catalogToolRepositoryStub) ImportAllProducts(_ context.Context, _ admin.Actor, dryRun bool) (admin.ImportResult, error) {
	stub.importAllCalls = append(stub.importAllCalls, dryRun)
	return admin.ImportResult{Created: 1, Entries: []admin.ImportEntry{
		{Code: "SABY-1", Status: "exists"},
		{Code: "SABY-2", Status: "new"},
	}}, nil
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

func TestImportAllProductsPassesDryRun(t *testing.T) {
	t.Parallel()

	repository := newCatalogToolRepositoryStub()
	request := adminRequest(http.MethodPost, "/api/v1/admin/products/import-all", `{"dryRun":true}`)
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), adminDependencies(repository, admin.RoleManager, "manager@example.com")).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if len(repository.importAllCalls) != 1 || !repository.importAllCalls[0] {
		t.Fatalf("bulk import calls = %v, want [true]", repository.importAllCalls)
	}
}

func TestImportAllProductsRejectsNonAdmin(t *testing.T) {
	t.Parallel()

	repository := newCatalogToolRepositoryStub()
	request := adminRequest(http.MethodPost, "/api/v1/admin/products/import-all", `{"dryRun":true}`)
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), adminDependencies(repository, "", "client@example.com")).ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
	if len(repository.importAllCalls) != 0 {
		t.Fatal("forbidden bulk import reached repository")
	}
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
