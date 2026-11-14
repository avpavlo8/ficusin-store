package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
)

type managerEditorPlanStub struct {
	marketplaceChannelsRepository
	actor     admin.Actor
	id        int64
	plan      admin.EditorPlan
	submitted bool
	err       error
}

func (s *managerEditorPlanStub) SaveEditorPlan(_ context.Context, actor admin.Actor, id int64, plan admin.EditorPlan, submit bool) (admin.EditorPlanView, error) {
	s.actor, s.id, s.plan, s.submitted = actor, id, plan, submit
	return admin.EditorPlanView{}, s.err
}

func TestEditorPlanShowsValidationAndHidesInternalErrors(t *testing.T) {
	for _, tt := range []struct {
		err    error
		status int
		want   string
		hidden string
	}{
		{fmt.Errorf("%w: формула цены изменилась; пересчитайте цену ozon перед сохранением", admin.ErrInvalidInput), 400, "формула цены изменилась", ""},
		{admin.ErrInvalidInput, 400, "Проверьте настройки площадок", ""},
		{errors.New("private database diagnostic"), 503, "Не удалось выполнить операцию", "private database diagnostic"},
	} {
		repo := &managerEditorPlanStub{err: tt.err}
		response := httptest.NewRecorder()
		NewRouter(discardLogger(), adminDependencies(repo, admin.RoleOwner, "")).ServeHTTP(response, adminRequest(http.MethodPut, "/api/v1/admin/products/61/editor-plan", `{"config":{"variants":[]},"submit":true}`))
		if response.Code != tt.status || !strings.Contains(response.Body.String(), tt.want) || tt.hidden != "" && strings.Contains(response.Body.String(), tt.hidden) {
			t.Fatalf("unexpected editor-plan error: %d %s", response.Code, response.Body.String())
		}
	}
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
