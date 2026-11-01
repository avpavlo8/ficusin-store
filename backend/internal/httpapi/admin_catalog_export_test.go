package httpapi

import (
	"context"
	"errors"
	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/catalogsync"
	"net/http"
	"net/http/httptest"
	"testing"
)

type exportServiceStub struct {
	called    int
	actor, id int64
	hash      string
	check     bool
	err       error
}

func (s *exportServiceStub) View(context.Context, int64) (catalogsync.View, error) {
	s.called++
	return catalogsync.View{Configured: true, State: "idle"}, s.err
}
func (s *exportServiceStub) Enqueue(_ context.Context, id, actor int64, hash string, check bool) (catalogsync.View, error) {
	s.called++
	s.id = id
	s.actor = actor
	s.hash = hash
	s.check = check
	return catalogsync.View{Configured: true, State: "queued"}, s.err
}
func TestCatalogueExportOwnerAndPreviewGuard(t *testing.T) {
	for _, role := range []string{admin.RoleOwner, admin.RoleManager} {
		svc := &exportServiceStub{}
		deps := adminDependencies(&adminRepositoryStub{}, role, "owner@example.com")
		deps.CatalogExport = svc
		r := adminRequest(http.MethodPost, "/api/v1/admin/variants/12/saby-export", `{"fingerprint":"saved-state","checkOnly":false}`)
		w := httptest.NewRecorder()
		NewRouter(discardLogger(), deps).ServeHTTP(w, r)
		if role == admin.RoleOwner {
			if w.Code != 202 || svc.id != 12 || svc.actor == 0 || svc.hash != "saved-state" {
				t.Fatalf("owner %d %+v %s", w.Code, svc, w.Body.String())
			}
		} else if w.Code != 403 || svc.called != 0 {
			t.Fatal("non-owner export allowed")
		}
	}
}
func TestCatalogueExportConflictAndCheckOnly(t *testing.T) {
	svc := &exportServiceStub{err: catalogsync.ErrConflict}
	deps := adminDependencies(&adminRepositoryStub{}, admin.RoleOwner, "owner@example.com")
	deps.CatalogExport = svc
	w := httptest.NewRecorder()
	NewRouter(discardLogger(), deps).ServeHTTP(w, adminRequest(http.MethodPost, "/api/v1/admin/variants/12/saby-export", `{"checkOnly":true}`))
	if w.Code != 409 || !svc.check || !errors.Is(svc.err, catalogsync.ErrConflict) {
		t.Fatalf("lost conflict/check mode: %d %+v", w.Code, svc)
	}
}
