package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
)

type removalRepository struct {
	*adminRepositoryStub
	removed []int64
}

func (r *removalRepository) RemoveProduct(_ context.Context, _ admin.Actor, id int64) (admin.ProductRemoval, error) {
	r.removed = append(r.removed, id)
	return admin.ProductRemoval{ProductID: id, Action: "archived"}, nil
}
func TestProductRemovalRequiresProductManagement(t *testing.T) {
	for _, scenario := range []struct {
		name, role string
		status     int
	}{
		{"owner", admin.RoleOwner, http.StatusOK},
		{"administrator", admin.RoleAdministrator, http.StatusOK},
		{"manager", admin.RoleManager, http.StatusOK},
		{"customer", "", http.StatusForbidden},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			repo := &removalRepository{adminRepositoryStub: &adminRepositoryStub{}}
			response := httptest.NewRecorder()
			NewRouter(discardLogger(), adminDependencies(repo, scenario.role, "")).ServeHTTP(response,
				adminRequest(http.MethodDelete, "/api/v1/admin/products/7", ""))
			if response.Code != scenario.status {
				t.Fatalf("status %d, want %d: %s", response.Code, scenario.status, response.Body.String())
			}
			if scenario.status == http.StatusForbidden {
				if len(repo.removed) != 0 {
					t.Fatalf("removal called without permission: %v", repo.removed)
				}
				return
			}
			if len(repo.removed) != 1 || repo.removed[0] != 7 || response.Body.String() != "{\"productId\":7,\"action\":\"archived\"}\n" {
				t.Fatalf("removed %v, response %s", repo.removed, response.Body.String())
			}
		})
	}
}
