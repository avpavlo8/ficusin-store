package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/procurement"
)

type namedDraftStub struct {
	procurementStub
	drafts map[int64]procurement.NamedPlanDraft
	nextID int64
}

func (stub *namedDraftStub) ListNamedPlanDrafts(context.Context, procurement.Actor) ([]procurement.NamedPlanDraft, error) {
	items := make([]procurement.NamedPlanDraft, 0, len(stub.drafts))
	for _, item := range stub.drafts {
		items = append(items, item)
	}
	return items, nil
}

func (stub *namedDraftStub) LoadNamedPlanDraft(_ context.Context, _ procurement.Actor, id int64) (procurement.NamedPlanDraft, error) {
	item, ok := stub.drafts[id]
	if !ok {
		return procurement.NamedPlanDraft{}, procurement.ErrNotFound
	}
	return item, nil
}

func (stub *namedDraftStub) CreateNamedPlanDraft(_ context.Context, _ procurement.Actor, title string, payload json.RawMessage) (procurement.NamedPlanDraft, error) {
	stub.nextID++
	item := procurement.NamedPlanDraft{ID: stub.nextID, Title: title, Payload: payload, UpdatedAt: time.Now()}
	stub.drafts[item.ID] = item
	return item, nil
}

func (stub *namedDraftStub) UpdateNamedPlanDraft(_ context.Context, _ procurement.Actor, id int64, title string, payload json.RawMessage) (procurement.NamedPlanDraft, error) {
	item, ok := stub.drafts[id]
	if !ok {
		return procurement.NamedPlanDraft{}, procurement.ErrNotFound
	}
	item.Title, item.Payload = title, payload
	stub.drafts[id] = item
	return item, nil
}

func (stub *namedDraftStub) DeleteNamedPlanDraft(_ context.Context, _ procurement.Actor, id int64) error {
	delete(stub.drafts, id)
	return nil
}

func TestSharedNamedProcurementDraftLifecycle(t *testing.T) {
	t.Parallel()
	stub := &namedDraftStub{drafts: make(map[int64]procurement.NamedPlanDraft)}
	owner := NewRouter(discardLogger(), procurementDependencies(stub, admin.RoleOwner))
	administrator := NewRouter(discardLogger(), procurementDependencies(stub, admin.RoleAdministrator))
	request := func(router http.Handler, method, path, body string, want int) string {
		t.Helper()
		response := httptest.NewRecorder()
		router.ServeHTTP(response, adminRequest(method, path, body))
		if response.Code != want {
			t.Fatalf("%s %s: status %d, want %d: %s", method, path, response.Code, want, response.Body.String())
		}
		return response.Body.String()
	}
	request(owner, http.MethodPost, "/api/v1/admin/procurement/plan-drafts", `{"title":"Первая закупка","payload":{"supplierId":1,"items":[{"id":"a"}]}}`, http.StatusCreated)
	request(owner, http.MethodPost, "/api/v1/admin/procurement/plan-drafts", `{"title":"Вторая закупка","payload":{"supplierId":2,"items":[{"id":"b"}]}}`, http.StatusCreated)
	if len(stub.drafts) != 2 {
		t.Fatalf("drafts = %d, want 2", len(stub.drafts))
	}
	request(administrator, http.MethodGet, "/api/v1/admin/procurement/plan-drafts", "", http.StatusOK)
	request(administrator, http.MethodPut, "/api/v1/admin/procurement/plan-drafts/1", `{"title":"Изменена администратором","payload":{"supplierId":1,"items":[{"id":"a"}]}}`, http.StatusOK)
	if stub.drafts[1].Title != "Изменена администратором" || stub.drafts[2].Title != "Вторая закупка" {
		t.Fatal("updating one draft changed another")
	}
	request(administrator, http.MethodDelete, "/api/v1/admin/procurement/plan-drafts/1", "", http.StatusNoContent)
	if len(stub.drafts) != 1 || stub.drafts[2].Title != "Вторая закупка" {
		t.Fatal("deleting one draft changed another")
	}
	request(NewRouter(discardLogger(), procurementDependencies(stub, admin.RoleManager)), http.MethodGet, "/api/v1/admin/procurement/plan-drafts", "", http.StatusForbidden)
}
