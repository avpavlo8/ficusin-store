package httpapi

import (
	"context"
	"errors"
	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"net/http"
	"net/http/httptest"
	"testing"
)

type packingReopenStub struct {
	*adminRepositoryStub
	calls    int
	revision int
	err      error
}

func (s *packingReopenStub) RequestOrderPackingReopen(_ context.Context, _ admin.Actor, id int64, revision int) (admin.OrderPackingState, error) {
	s.calls++
	s.revision = revision
	return admin.OrderPackingState{Revision: revision, State: "reopening"}, s.err
}
func (s *packingReopenStub) OrderForPacking(context.Context, int64) (admin.Order, error) {
	return admin.Order{ID: 7}, nil
}
func TestPackingReopenEndpoint(t *testing.T) {
	for _, scenario := range []string{"manager", "owner", "forbidden", "invalid", "carrier_conflict"} {
		t.Run(scenario, func(t *testing.T) {
			repo := &packingReopenStub{adminRepositoryStub: &adminRepositoryStub{}}
			role := admin.RoleOwner
			body := `{"revision":3}`
			if scenario == "manager" {
				role = admin.RoleManager
			}
			if scenario == "forbidden" {
				role = "customer"
			}
			if scenario == "invalid" {
				body = `{"revision":0}`
			}
			if scenario == "carrier_conflict" {
				repo.err = errors.New("carrier still creating")
			}
			response := httptest.NewRecorder()
			NewRouter(discardLogger(), adminDependencies(repo, role, "operator@example.com")).ServeHTTP(response, adminRequest(http.MethodPost, "/api/v1/admin/orders/7/packing/reopen", body))
			want := http.StatusAccepted
			switch scenario {
			case "forbidden":
				want = http.StatusForbidden
			case "invalid":
				want = http.StatusBadRequest
			case "carrier_conflict":
				want = http.StatusConflict
			}
			if response.Code != want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, want, response.Body.String())
			}
			if (scenario == "forbidden" || scenario == "invalid") && repo.calls != 0 {
				t.Fatal("invalid request caused mutation")
			}
			if repo.calls > 0 && repo.revision != 3 {
				t.Fatal("revision lost")
			}
		})
	}
}
