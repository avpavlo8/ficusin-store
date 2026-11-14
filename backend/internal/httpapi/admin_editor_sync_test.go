package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
)

type editorSyncStub struct {
	adminRepositoryStub
	called bool
	actor  admin.Actor
	id     int64
	err    error
}

func (s *editorSyncStub) SyncExistingEditorPlan(_ context.Context, actor admin.Actor, id int64) (admin.EditorSyncResult, error) {
	s.called, s.actor, s.id = true, actor, id
	return admin.EditorSyncResult{Queued: 1, Skipped: 1, Warnings: []string{"Связь требует проверки"}}, s.err
}

func TestEditorSyncManagerUsesAuthenticatedActorAndServerProduct(t *testing.T) {
	stub := &editorSyncStub{}
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), adminDependencies(stub, admin.RoleManager, "manager@example.com")).ServeHTTP(response,
		adminRequest(http.MethodPost, "/api/v1/admin/products/61/editor-plan/sync-existing", `{}`))
	if response.Code != http.StatusOK || !stub.called || stub.id != 61 || stub.actor.CustomerID != 1 || stub.actor.Role != admin.RoleManager {
		t.Fatalf("status=%d called=%v product=%d actor=%+v body=%s", response.Code, stub.called, stub.id, stub.actor, response.Body.String())
	}
	var result admin.EditorSyncResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Queued != 1 || result.Skipped != 1 || len(result.Warnings) != 1 {
		t.Fatalf("sync outcome was lost: result=%+v err=%v", result, err)
	}
}

func TestEditorSyncRejectsClientConfigurationBeforeCallingRepository(t *testing.T) {
	for _, body := range []string{
		`{"config":{"variants":[]}}`,
		`{"actorId":2}`,
		`{"productId":62}`,
		`{"priceMinor":1,"existingId":"other-card"}`,
		`{} {}`,
		``,
	} {
		t.Run(body, func(t *testing.T) {
			stub := &editorSyncStub{}
			response := httptest.NewRecorder()
			NewRouter(discardLogger(), adminDependencies(stub, admin.RoleManager, "manager@example.com")).ServeHTTP(response,
				adminRequest(http.MethodPost, "/api/v1/admin/products/61/editor-plan/sync-existing", body))
			if response.Code != http.StatusBadRequest || stub.called {
				t.Fatalf("status=%d called=%v body=%s", response.Code, stub.called, response.Body.String())
			}
		})
	}
}

func TestEditorSyncDoesNotGrantAccessToOtherProductOperations(t *testing.T) {
	for _, tc := range []struct {
		name, role, method, path string
	}{
		{"customer", "", http.MethodPost, "/api/v1/admin/products/61/editor-plan/sync-existing"},
		{"unknown role", "viewer", http.MethodPost, "/api/v1/admin/products/61/editor-plan/sync-existing"},
		{"administrator cannot write plan", admin.RoleAdministrator, http.MethodPut, "/api/v1/admin/products/61/editor-plan"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &editorSyncStub{}
			response := httptest.NewRecorder()
			NewRouter(discardLogger(), adminDependencies(stub, tc.role, "user@example.com")).ServeHTTP(response, adminRequest(tc.method, tc.path, `{}`))
			if response.Code != http.StatusForbidden || stub.called {
				t.Fatalf("status=%d called=%v body=%s", response.Code, stub.called, response.Body.String())
			}
		})
	}
}

func TestEditorSyncPreservesDomainPermissionCheck(t *testing.T) {
	stub := &editorSyncStub{err: admin.ErrForbidden}
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), adminDependencies(stub, admin.RoleManager, "manager@example.com")).ServeHTTP(response,
		adminRequest(http.MethodPost, "/api/v1/admin/products/61/editor-plan/sync-existing", `{}`))
	if response.Code != http.StatusForbidden || !stub.called {
		t.Fatalf("status=%d called=%v body=%s", response.Code, stub.called, response.Body.String())
	}
}
