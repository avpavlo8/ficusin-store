package httpapi

import (
	"context"
	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"net/http/httptest"
	"strings"
	"testing"
)

type crmProductRepository struct {
	adminRepositoryStub
	updates      []admin.ProductUpdate
	catalogReads int
}

func (s *crmProductRepository) ListProducts(context.Context) ([]admin.Product, error) {
	s.catalogReads++
	return []admin.Product{{ID: 1, Image: "/one.webp"}}, nil
}
func (s *crmProductRepository) UpdateProduct(_ context.Context, _ admin.Actor, _ int64, u admin.ProductUpdate) (admin.Product, error) {
	s.updates = append(s.updates, u)
	return admin.Product{ID: 1}, nil
}
func TestCRMManagerDeniedRoutes(t *testing.T) {
	cases := []struct{ method, path, body string }{
		{"GET", "/api/v1/admin/analytics", ""}, {"GET", "/api/v1/admin/settings", ""},
		{"PATCH", "/api/v1/admin/procurement/order-lines/18", `{"invoiceLineIds":[1,2]}`},
		{"POST", "/api/v1/admin/procurement/orders", "{}"},
		{"GET", "/api/v1/admin/procurement/orders/18/saby-prices.xlsx", ""},
		{"POST", "/api/v1/admin/procurement/orders/18/saby-prices.xlsx", `{ "firstPrices": [{"sabyId":"3892","priceRub":636}] }`},
		{"GET", "/api/v1/admin/procurement/orders/18/receiving.pdf", ""},
		{"POST", "/api/v1/admin/procurement/batches/1/retry", "{}"},
		{"POST", "/api/v1/admin/products/import", `{"codes":["a"]}`},
		{"POST", "/api/v1/admin/products/merge", "{}"},
		{"DELETE", "/api/v1/admin/products", `{"productIds":[1]}`},
		{"DELETE", "/api/v1/admin/categories/1", ""},

		{"DELETE", "/api/v1/admin/collection-definitions/1", ""},
		{"PATCH", "/api/v1/admin/customers/1", `{"fullName":"Changed","retailDiscountBps":9000}`},
	}
	for _, c := range cases {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			router := NewRouter(discardLogger(), adminDependencies(admin.NewPostgresRepository(nil), admin.RoleManager, ""))
			response := httptest.NewRecorder()
			router.ServeHTTP(response, adminRequest(c.method, c.path, c.body))
			if response.Code != 403 {
				t.Fatalf("got %d: %s", response.Code, response.Body.String())
			}
		})
	}
}
func TestCRMAdministratorProductPatchRejectsEntireMixedRequest(t *testing.T) {
	for _, field := range []string{`"variants":[{"id":1,"priceMinor":1}]`, `"Variants":[{"id":1,"priceMinor":1}]`, `"variants":[{"id":1,"active":true}]`, `"variants":[{"id":1,"externalIds":[]}]`, `"variants":[{"id":1,"wholesaleMinQty":2}]`, `"status":"archived"`, `"unknown":1`} {
		t.Run(field, func(t *testing.T) {
			repository := &crmProductRepository{}
			router := NewRouter(discardLogger(), adminDependencies(repository, admin.RoleAdministrator, ""))
			response := httptest.NewRecorder()
			router.ServeHTTP(response, adminRequest("PATCH", "/api/v1/admin/products/1", `{"name":"Must not persist",`+field+`}`))
			if response.Code != 403 || len(repository.updates) != 0 {
				t.Fatalf("status=%d writes=%d", response.Code, len(repository.updates))
			}
		})
	}
}
func TestCRMContentEditUsesOnlyAtomicProductPatch(t *testing.T) {
	for _, role := range []string{admin.RoleOwner, admin.RoleManager} {
		repository := &crmProductRepository{}
		body := `{"description":"Updated content"}`
		router := NewRouter(discardLogger(), adminDependencies(repository, role, ""))
		response := httptest.NewRecorder()
		router.ServeHTTP(response, adminRequest("PATCH", "/api/v1/admin/products/1", body))
		if response.Code != 200 || repository.catalogReads != 0 || len(repository.updates) != 1 || repository.updates[0].Description == nil || *repository.updates[0].Description != "Updated content" {
			t.Fatalf("role=%s status=%d updates=%+v", role, response.Code, repository.updates)
		}
	}
}
func TestCRMManagerPermissionResponse(t *testing.T) {
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), adminDependencies(&adminRepositoryStub{}, admin.RoleManager, "")).ServeHTTP(response, adminRequest("GET", "/api/v1/admin/dashboard", ""))
	if !strings.Contains(response.Body.String(), "products.manage") {
		t.Fatal("full editor permission missing")
	}
	for _, forbidden := range []string{"analytics.read", "procurement.edit", "catalog.delete"} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatalf("leaked permission %s", forbidden)
		}
	}
}

func TestManagerUnifiedCommercialPatchAndMediaDelete(t *testing.T) {
	repo := &crmProductRepository{}
	w := httptest.NewRecorder()
	body := `{"name":"Updated","variants":[{"id":1,"priceMinor":12345,"wholesaleMinQty":3},{"id":-1,"label":"D17","priceMinor":23456},{"id":2,"remove":true}]}`
	NewRouter(discardLogger(), adminDependencies(repo, admin.RoleManager, "")).ServeHTTP(w, adminRequest("PATCH", "/api/v1/admin/products/1", body))
	if w.Code != 200 || len(repo.updates) != 1 || len(repo.updates[0].Variants) != 3 || repo.updates[0].Variants[0].PriceMinor == nil || *repo.updates[0].Variants[0].PriceMinor != 12345 || repo.updates[0].Variants[0].WholesaleMinQty == nil || *repo.updates[0].Variants[0].WholesaleMinQty != 3 || !repo.updates[0].Variants[2].Remove {
		t.Fatalf("manager editor denied: %d %s", w.Code, w.Body.String())
	}
	for _, role := range []string{admin.RoleManager, admin.RoleAdministrator} {
		media := newCatalogToolRepositoryStub()
		w = httptest.NewRecorder()
		NewRouter(discardLogger(), adminDependencies(media, role, "")).ServeHTTP(w, adminRequest("DELETE", "/api/v1/admin/products/1/media/2", ""))
		want := 200
		if role == admin.RoleAdministrator {
			want = 403
		}
		if w.Code != want {
			t.Fatalf("role=%s media=%d", role, w.Code)
		}
	}
}

func TestManagerUnifiedEditorRejectsManualStockAtomically(t *testing.T) {
	for _, field := range []string{`"stock":null`, `"Stock":0`, `"sabyFields":[]`, `"SabyFields":null`} {
		repo := &crmProductRepository{}
		w := httptest.NewRecorder()
		NewRouter(discardLogger(), adminDependencies(repo, admin.RoleManager, "")).ServeHTTP(w, adminRequest("PATCH", "/api/v1/admin/products/1", `{"name":"Must not save",`+field+`}`))
		if w.Code != 403 || len(repo.updates) != 0 {
			t.Fatalf("stock field %s bypassed: %d", field, w.Code)
		}
	}
	for _, field := range []string{`"stock":null`, `"Stock":0`} {
		repo := &crmProductRepository{}
		w := httptest.NewRecorder()
		NewRouter(discardLogger(), adminDependencies(repo, admin.RoleManager, "")).ServeHTTP(w, adminRequest("PATCH", "/api/v1/admin/products/1", `{"name":"Must not save","variants":[{"id":1,"priceMinor":100,`+field+`}]}`))
		if w.Code != 400 || len(repo.updates) != 0 {
			t.Fatalf("unsupported unified stock %s bypassed: %d", field, w.Code)
		}
		w = httptest.NewRecorder()
		NewRouter(discardLogger(), adminDependencies(admin.NewPostgresRepository(nil), admin.RoleManager, "")).ServeHTTP(w, adminRequest("POST", "/api/v1/admin/products/1/variants", `{"label":"D13",`+field+`}`))
		if w.Code != 403 {
			t.Fatalf("explicit create stock %s bypassed: %d", field, w.Code)
		}
	}
}
