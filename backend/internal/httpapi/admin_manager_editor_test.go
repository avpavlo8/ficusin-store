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

func TestStaffCanSubmitFullEditorPlanWithExternalLinks(t *testing.T) {
	body := `{"config":{"variants":[{"variantId":71,"saby":{"mode":"link","existingCode":"X100"},"marketplace":{"wb":{"enabled":true,"mode":"link","existingId":"wb-card"},"ozon":{"enabled":true,"mode":"link","existingId":"ozon-card"}}}]},"submit":true}`
	for _, role := range []string{admin.RoleManager, admin.RoleAdministrator} {
		t.Run(role, func(t *testing.T) {
			repo := &managerEditorPlanStub{}
			response := httptest.NewRecorder()
			NewRouter(discardLogger(), adminDependencies(repo, role, "")).ServeHTTP(response,
				adminRequest(http.MethodPut, "/api/v1/admin/products/61/editor-plan", body))
			if response.Code != http.StatusOK || !repo.submitted || repo.id != 61 ||
				repo.actor.Role != role || repo.actor.CustomerID != 1 || len(repo.plan.Variants) != 1 {
				t.Fatalf("editor plan not submitted: %d %+v", response.Code, repo)
			}
			variant := repo.plan.Variants[0]
			if variant.VariantID != 71 || variant.Saby.ExistingCode != "X100" ||
				variant.Marketplace.WB.ExistingID != "wb-card" || variant.Marketplace.Ozon.ExistingID != "ozon-card" {
				t.Fatalf("external links not forwarded: %+v", variant)
			}
		})
	}
}
