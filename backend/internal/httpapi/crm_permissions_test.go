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
	updates []admin.ProductUpdate
}

func (s *crmProductRepository) ListProducts(context.Context) ([]admin.Product, error) {
	return []admin.Product{{ID: 1, Image: "/one.webp"}}, nil
}
func (s *crmProductRepository) UpdateProduct(_ context.Context, _ admin.Actor, _ int64, u admin.ProductUpdate) (admin.Product, error) {
	s.updates = append(s.updates, u)
	return admin.Product{ID: 1}, nil
}
func TestCRMManagerDeniedRoutes(t *testing.T) {
	cases := []struct{ method, path, body string }{
		{"GET", "/api/v1/admin/analytics", ""}, {"GET", "/api/v1/admin/settings", ""},
		{"GET", "/api/v1/admin/procurement", ""}, {"GET", "/api/v1/admin/procurement/orders/18", ""},
		{"GET", "/api/v1/admin/procurement/orders/18/saby-prices.xlsx", ""},
		{"POST", "/api/v1/admin/procurement/batches/1/retry", "{}"},
		{"POST", "/api/v1/admin/products/sync", `{"productIds":[1],"fields":["price"]}`},
		{"POST", "/api/v1/admin/products/import", `{"codes":["a"]}`},
		{"POST", "/api/v1/admin/products", "{}"}, {"POST", "/api/v1/admin/products/merge", "{}"},
		{"DELETE", "/api/v1/admin/products", `{"productIds":[1]}`},
		{"DELETE", "/api/v1/admin/categories/1", ""},
		{"DELETE", "/api/v1/admin/products/1/media/2", ""},
		{"DELETE", "/api/v1/admin/variants/1/media/2", ""},
		{"POST", "/api/v1/admin/variants/1/copy", "{}"},
		{"POST", "/api/v1/admin/variants/1/archive", "{}"},
		{"DELETE", "/api/v1/admin/variants/1", ""},
		{"POST", "/api/v1/admin/products/1/variants", "{}"},
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
func TestCRMManagerProductPatchRejectsEntireMixedRequest(t *testing.T) {
	for _, field := range []string{`"priceMinor":1`, `"PriceMinor":1`, `"stock":null`, `"externalIds":[]`, `"sabyFields":[]`, `"wholesaleMinQty":2`, `"image":"/replacement.webp"`, `"status":"archived"`, `"unknown":1`} {
		t.Run(field, func(t *testing.T) {
			repository := &crmProductRepository{}
			router := NewRouter(discardLogger(), adminDependencies(repository, admin.RoleManager, ""))
			response := httptest.NewRecorder()
			router.ServeHTTP(response, adminRequest("PATCH", "/api/v1/admin/products/1", `{"name":"Must not persist",`+field+`}`))
			if response.Code != 403 || len(repository.updates) != 0 {
				t.Fatalf("status=%d writes=%d", response.Code, len(repository.updates))
			}
		})
	}
}
func TestCRMManagerVariantMixedPatchRejected(t *testing.T) {
	for _, field := range []string{`"priceMinor":null`, `"stock":0`, `"externalIds":[]`, `"active":false`, `"PriceMinor":10`} {
		router := NewRouter(discardLogger(), adminDependencies(admin.NewPostgresRepository(nil), admin.RoleManager, ""))
		response := httptest.NewRecorder()
		router.ServeHTTP(response, adminRequest("PATCH", "/api/v1/admin/variants/1", `{"label":"Must not persist",`+field+`}`))
		if response.Code != 403 {
			t.Fatalf("%s: %d %s", field, response.Code, response.Body.String())
		}
	}
}
func TestCRMContentEditPreservesGallery(t *testing.T) {
	for _, role := range []string{admin.RoleOwner, admin.RoleManager} {
		repository := &crmProductRepository{}
		body := `{"description":"Updated content"}`
		if role == admin.RoleOwner {
			body = `{"description":"Updated content","image":"/one.webp"}`
		}
		router := NewRouter(discardLogger(), adminDependencies(repository, role, ""))
		response := httptest.NewRecorder()
		router.ServeHTTP(response, adminRequest("PATCH", "/api/v1/admin/products/1", body))
		if response.Code != 200 || len(repository.updates) != 1 || repository.updates[0].Image != nil {
			t.Fatalf("role=%s status=%d updates=%+v", role, response.Code, repository.updates)
		}
	}
}
func TestCRMManagerPermissionResponse(t *testing.T) {
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), adminDependencies(&adminRepositoryStub{}, admin.RoleManager, "")).ServeHTTP(response, adminRequest("GET", "/api/v1/admin/dashboard", ""))
	for _, forbidden := range []string{"analytics.read", "procurement.read", "procurement.edit", "products.sync", "products.manage", "catalog.delete"} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatalf("leaked permission %s", forbidden)
		}
	}
}
