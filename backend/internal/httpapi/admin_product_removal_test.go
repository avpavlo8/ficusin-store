package httpapi

import (
	"context"
	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"net/http/httptest"
	"testing"
)

type removalRepository struct {
	*adminRepositoryStub
	removed []int64
}

func (r *removalRepository) RemoveProduct(_ context.Context, _ admin.Actor, id int64) (admin.ProductRemoval, error) {
	r.removed = append(r.removed, id)
	return admin.ProductRemoval{ProductID: id, Action: "archived"}, nil
}
func TestProductRemovalRequiresExistingDeletePermission(t *testing.T) {
	for _, role := range []string{admin.RoleOwner, admin.RoleManager, admin.RoleAdministrator} {
		repo := &removalRepository{adminRepositoryStub: &adminRepositoryStub{}}
		w := httptest.NewRecorder()
		NewRouter(discardLogger(), adminDependencies(repo, role, "")).ServeHTTP(w, adminRequest("DELETE", "/api/v1/admin/products/7", ""))
		want := 403
		if role == admin.RoleOwner {
			want = 200
		}
		if w.Code != want || len(repo.removed) != map[bool]int{true: 1, false: 0}[role == admin.RoleOwner] {
			t.Fatalf("%s status %d writes %v", role, w.Code, repo.removed)
		}
		if role == admin.RoleOwner && w.Body.String() != "{\"productId\":7,\"action\":\"archived\"}\n" {
			t.Fatal(w.Body.String())
		}
	}
}
