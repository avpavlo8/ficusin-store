package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/operations"
)

type operationsStub struct {
	snapshot operations.Snapshot
	err      error
}

func (stub operationsStub) Snapshot(context.Context) (operations.Snapshot, error) {
	return stub.snapshot, stub.err
}

func TestPublicOperationsHealthHidesBusinessCounts(t *testing.T) {
	t.Parallel()
	dependencies := testDependencies(catalogStub{}, authStub{})
	dependencies.Operations = operationsStub{snapshot: operations.Snapshot{
		Status: "degraded", GeneratedAt: time.Now(),
		Checks: []operations.Check{{Code: "stale_outbox", Severity: "warning", Affected: 27}},
	}}
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), dependencies).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/operations/health", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "degraded" {
		t.Fatalf("unexpected body: %#v", body)
	}
	if _, exposed := body["checks"]; exposed {
		t.Fatalf("public response exposed operational details: %#v", body)
	}
}

func TestCriticalOperationsHealthReturnsServiceUnavailable(t *testing.T) {
	t.Parallel()
	dependencies := testDependencies(catalogStub{}, authStub{})
	dependencies.Operations = operationsStub{snapshot: operations.Snapshot{
		Status: "unavailable",
		Checks: []operations.Check{{Code: "duplicate_paid_payments", Severity: "critical", Affected: 1}},
	}}
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), dependencies).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/operations/health", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestAdminOperationsRequiresDashboardPermission(t *testing.T) {
	t.Parallel()
	dependencies := adminDependencies(&adminRepositoryStub{}, admin.RoleManager, "manager@example.com")
	dependencies.Operations = operationsStub{snapshot: operations.Snapshot{Status: "ok"}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/operations", nil)
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), dependencies).ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}
