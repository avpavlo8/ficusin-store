package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
)

type channelScopeStub struct {
	adminRepositoryStub
	called bool
	scope  string
}

func (stub *channelScopeStub) SetVariantMarketplaceScope(_ context.Context, _ admin.Actor, id int64, scope string) error {
	stub.called = id == 61
	stub.scope = scope
	return nil
}

func TestMarketplaceScopeRequiresOwner(t *testing.T) {
	stub := &channelScopeStub{}
	request := adminRequest(http.MethodPatch, "/api/v1/admin/variants/61/marketplace-scope", `{"marketplaceScope":"site_only"}`)
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), adminDependencies(stub, admin.RoleManager, "manager@example.com")).ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || stub.called {
		t.Fatalf("status=%d called=%v", response.Code, stub.called)
	}
}

func TestMarketplaceScopeUpdatesVariant(t *testing.T) {
	stub := &channelScopeStub{}
	request := adminRequest(http.MethodPatch, "/api/v1/admin/variants/61/marketplace-scope", `{"marketplaceScope":"site_only"}`)
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), adminDependencies(stub, admin.RoleOwner, "owner@example.com")).ServeHTTP(response, request)
	if response.Code != http.StatusOK || !stub.called || stub.scope != "site_only" {
		t.Fatalf("status=%d called=%v scope=%q body=%s", response.Code, stub.called, stub.scope, response.Body.String())
	}
}

func TestMarketplaceScopeRejectsUnknownValue(t *testing.T) {
	stub := &channelScopeStub{}
	request := adminRequest(http.MethodPatch, "/api/v1/admin/variants/61/marketplace-scope", `{"marketplaceScope":"all"}`)
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), adminDependencies(stub, admin.RoleOwner, "owner@example.com")).ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || stub.called {
		t.Fatalf("status=%d called=%v", response.Code, stub.called)
	}
}
