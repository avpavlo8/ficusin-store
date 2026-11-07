package httpapi

import (
	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProductCreationIsDisabledOutsideStaging(t *testing.T) {
	repo := &adminRepositoryStub{}
	deps := adminDependencies(repo, admin.RoleOwner, "owner@example.com")
	deps.Environment = "production"
	handler := NewRouter(discardLogger(), deps)
	for _, row := range []struct {
		method, path, body string
		status             int
	}{{"GET", "/api/v1/admin/product-creations/options", "", 200}, {"POST", "/api/v1/admin/product-creations", "{}", 404}, {"GET", "/api/v1/product-creation-photos/random", "", 404}} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, adminRequest(row.method, row.path, row.body))
		if response.Code != row.status {
			t.Fatalf("%s %s status %d: %s", row.method, row.path, response.Code, response.Body.String())
		}
		if row.status == 200 && !strings.Contains(response.Body.String(), `"enabled":false`) {
			t.Fatal("production exposed staging simulation")
		}
	}
	if len(repo.createdProducts) > 0 {
		t.Fatal("production product was modified")
	}
}
