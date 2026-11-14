package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
)

type managerEditorPlanStub struct {
	marketplaceChannelsRepository
	actor     admin.Actor
	id        int64
	plan      admin.EditorPlan
	submitted bool
}

func (s *managerEditorPlanStub) SaveEditorPlan(_ context.Context, actor admin.Actor, id int64, plan admin.EditorPlan, submit bool) (admin.EditorPlanView, error) {
	s.actor, s.id, s.plan, s.submitted = actor, id, plan, submit
	return admin.EditorPlanView{}, nil
}

func TestManagerCanSubmitFullEditorPlanWithoutGrantingAdministrator(t *testing.T) {
	for _, role := range []string{admin.RoleManager, admin.RoleAdministrator} {
		repo := &managerEditorPlanStub{}
		response := httptest.NewRecorder()
		NewRouter(discardLogger(), adminDependencies(repo, role, "")).ServeHTTP(response, adminRequest(http.MethodPut, "/api/v1/admin/products/61/editor-plan", `{"config":{"variants":[{"variantId":71,"saby":{"mode":"create","category":{"id":"1041"},"folder":{"id":"3333"}},"marketplace":{"wb":{"enabled":true,"mode":"create","category":{"id":"936"}}},"wbPrice":"1200"}]},"submit":true}`))
		if role == admin.RoleAdministrator {
			if response.Code != 403 || repo.submitted {
				t.Fatalf("administrator upgraded: %d", response.Code)
			}
			continue
		}
		if response.Code != 200 || !repo.submitted || repo.id != 61 || repo.actor.Role != admin.RoleManager || repo.actor.CustomerID != 1 || len(repo.plan.Variants) != 1 || repo.plan.Variants[0].WBPrice != "1200" {
			t.Fatalf("manager plan not accepted: %d %+v", response.Code, repo)
		}
	}
}
