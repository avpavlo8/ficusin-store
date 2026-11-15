package httpapi

import (
	"context"
	"encoding/json"
	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"net/http"
	"net/http/httptest"
	"testing"
)

type flowwowReadStub struct{ calls int }

func (s *flowwowReadStub) List(context.Context) (any, error) { s.calls++; return map[string]any{}, nil }
func (s *flowwowReadStub) Reference(context.Context, string, string) (json.RawMessage, error) {
	s.calls++
	return json.RawMessage(`{}`), nil
}
func (s *flowwowReadStub) Remote(context.Context, int) (any, error) {
	s.calls++
	return map[string]any{}, nil
}
func TestFlowwowReadHandlersRejectAnonymousBeforeAccess(t *testing.T) {
	for _, kind := range []string{"list", "references", "products"} {
		s := &flowwowReadStub{}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/v1/admin/flowwow?page=0", nil)
		h := adminHandlers{auth: adminAuthStub{}}
		flowwowHandler(h, s, kind)(w, r)
		if w.Code != 401 || s.calls != 0 {
			t.Fatalf("%s: status %d, calls %d", kind, w.Code, s.calls)
		}
	}
}

func TestFlowwowBindingHandlersRejectAnonymousBeforeAccess(t *testing.T) {
	for _, kind := range []string{"candidates", "preview", "confirm"} {
		s := &flowwowReadStub{}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/v1/admin/flowwow/links", nil)
		h := adminHandlers{auth: adminAuthStub{}}
		if kind == "candidates" {
			flowwowCandidatesHandler(h, s)(w, r)
		} else {
			flowwowLinkHandler(h, s, kind == "confirm")(w, r)
		}
		if w.Code != 401 || s.calls != 0 {
			t.Fatalf("%s status=%d calls=%d", kind, w.Code, s.calls)
		}
	}
}

type flowwowLinkSourceStub struct{ flowwowReadStub }

func (s *flowwowLinkSourceStub) Resolve(_ context.Context, id int64) (integration.FlowwowProduct, string, error) {
	s.calls++
	return integration.FlowwowProduct{ProductID: id, Type: 1, SubCategoryID: 101}, "1", nil
}

type flowwowLinkRepoStub struct {
	adminRepositoryStub
	called bool
	apply  bool
	input  admin.FlowwowLinkInput
	actor  admin.Actor
}

func (s *flowwowLinkRepoStub) FlowwowLink(_ context.Context, a admin.Actor, in admin.FlowwowLinkInput, remote integration.FlowwowProduct, shop string, apply bool) (admin.FlowwowLinkView, error) {
	s.called = true
	s.apply = apply
	s.actor = a
	s.input = in
	return admin.FlowwowLinkView{Remote: remote, ShopID: shop, Queued: apply}, nil
}
func TestFlowwowLinkUsesAuthenticatedActorAndConfirmation(t *testing.T) {
	for _, confirm := range []bool{false, true} {
		repo := &flowwowLinkRepoStub{}
		source := &flowwowLinkSourceStub{}
		deps := adminDependencies(repo, admin.RoleOwner, "owner@example.com")
		deps.Flowwow = source
		path := "/api/v1/admin/flowwow/links"
		if !confirm {
			path += "/preview"
		}
		response := httptest.NewRecorder()
		NewRouter(discardLogger(), deps).ServeHTTP(response, adminRequest(http.MethodPost, path, `{"variantId":210,"remoteId":900001,"fingerprint":"proof"}`))
		if response.Code != 200 || !repo.called || repo.apply != confirm || repo.input.VariantID != 210 || repo.actor.CustomerID != 1 || source.calls != 1 {
			t.Fatalf("status=%d repo=%+v calls=%d body=%s", response.Code, repo, source.calls, response.Body.String())
		}
	}
	for _, role := range []string{admin.RoleManager, admin.RoleAdministrator} {
		repo := &flowwowLinkRepoStub{}
		source := &flowwowLinkSourceStub{}
		deps := adminDependencies(repo, role, "user@example.com")
		deps.Flowwow = source
		response := httptest.NewRecorder()
		NewRouter(discardLogger(), deps).ServeHTTP(response, adminRequest(http.MethodPost, "/api/v1/admin/flowwow/links", `{"variantId":210,"remoteId":900001}`))
		if response.Code != 403 || repo.called || source.calls != 0 {
			t.Fatal("unauthorized binding reached source", response.Code, repo.called, source.calls)
		}
	}
}
