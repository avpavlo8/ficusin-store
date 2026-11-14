package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/ordersale"
)

type realizationHTTPRepository struct {
	adminRepositoryStub
	lists, preparations int
	actor               admin.Actor
	input               ordersale.PrepareInput
	err                 error
	retries             int
	retryID             int64
}

func (r *realizationHTTPRepository) RetryOrderRealization(_ context.Context, actor admin.Actor, id int64) error {
	r.retries++
	r.retryID, r.actor = id, actor
	return r.err
}

func TestOrderRealizationRetryRejectsUnsafeCreationWithoutBypassingOwner(t *testing.T) {
	for _, scenario := range []struct {
		name, role    string
		err           error
		status, calls int
	}{
		{"manager", admin.RoleManager, nil, http.StatusForbidden, 0},
		{"safe readback", admin.RoleOwner, nil, http.StatusOK, 1},
		{"unknown create", admin.RoleOwner, ordersale.ErrRetryUnsafe, http.StatusConflict, 1},
		{"missing publication", admin.RoleOwner, ordersale.ErrInput, http.StatusBadRequest, 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			repo := &realizationHTTPRepository{err: scenario.err}
			response := httptest.NewRecorder()
			NewRouter(discardLogger(), adminDependencies(repo, scenario.role, "operator@example.invalid")).ServeHTTP(response, adminRequest(http.MethodPost, "/api/v1/admin/order-realizations/7/retry", ""))
			if response.Code != scenario.status || repo.retries != scenario.calls {
				t.Fatalf("status=%d retries=%d: %s", response.Code, repo.retries, response.Body.String())
			}
			if repo.retries > 0 && (repo.actor.CustomerID != 1 || repo.retryID != 7) {
				t.Fatalf("retry lost actor or publication: %+v", repo)
			}
		})
	}
}

func (r *realizationHTTPRepository) OrderRealizations(_ context.Context, actor admin.Actor, _ string, _ int) (ordersale.View, error) {
	r.lists++
	r.actor = actor
	return ordersale.View{Events: []ordersale.Event{}, Warehouses: []ordersale.Warehouse{}}, r.err
}
func (r *realizationHTTPRepository) PrepareOrderRealizations(_ context.Context, actor admin.Actor, input ordersale.PrepareInput) ([]ordersale.Event, error) {
	r.preparations++
	r.actor, r.input = actor, input
	return []ordersale.Event{{IntentID: 7, State: "prepared"}}, r.err
}

func TestOrderRealizationsRequireOwnerBeforeReadingOrPreparing(t *testing.T) {
	for _, role := range []string{"", admin.RoleManager} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			t.Run(role+method, func(t *testing.T) {
				repo := &realizationHTTPRepository{}
				path := "/api/v1/admin/order-realizations"
				if method == http.MethodPost {
					path += "/prepare"
				}
				response := httptest.NewRecorder()
				NewRouter(discardLogger(), adminDependencies(repo, role, "operator@example.invalid")).ServeHTTP(response, adminRequest(method, path, ""))
				if response.Code != http.StatusForbidden || repo.lists != 0 || repo.preparations != 0 {
					t.Fatalf("status=%d reads=%d writes=%d: %s", response.Code, repo.lists, repo.preparations, response.Body.String())
				}
			})
		}
	}
}

func TestOrderRealizationRejectsMalformedSelectionBeforePreparing(t *testing.T) {
	fingerprint := strings.Repeat("a", 64)
	for _, body := range []string{
		`{`,
		`{"warehouseId":1,"events":[]}`,
		`{"warehouseId":0,"events":[{"orderId":1}]}`,
		fmt.Sprintf(`{"warehouseId":1,"events":[{"orderId":0,"fingerprint":%q}]}`, fingerprint),
		fmt.Sprintf(`{"warehouseId":1,"events":[{"orderId":1,"shipmentOfferId":-1,"fingerprint":%q}]}`, fingerprint),
		`{"warehouseId":1,"events":[{"orderId":1,"fingerprint":"stale"}]}`,
	} {
		t.Run(body, func(t *testing.T) {
			repo := &realizationHTTPRepository{}
			response := httptest.NewRecorder()
			NewRouter(discardLogger(), adminDependencies(repo, admin.RoleOwner, "owner@example.invalid")).ServeHTTP(response, adminRequest(http.MethodPost, "/api/v1/admin/order-realizations/prepare", body))
			if response.Code != http.StatusBadRequest || repo.preparations != 0 {
				t.Fatalf("status=%d preparations=%d: %s", response.Code, repo.preparations, response.Body.String())
			}
		})
	}
}

func TestOrderRealizationPreparationReportsConflictAndNoExternalCreation(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		err    error
		status int
	}{
		{"prepared", nil, http.StatusOK},
		{"changed quantities", ordersale.ErrConflict, http.StatusConflict},
		{"handover unconfirmed", ordersale.ErrNotReady, http.StatusConflict},
		{"inactive warehouse", ordersale.ErrInput, http.StatusBadRequest},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			repo := &realizationHTTPRepository{err: scenario.err}
			body := fmt.Sprintf(`{"warehouseId":3,"events":[{"orderId":4,"shipmentOfferId":5,"fingerprint":%q}]}`, strings.Repeat("a", 64))
			response := httptest.NewRecorder()
			NewRouter(discardLogger(), adminDependencies(repo, admin.RoleOwner, "owner@example.invalid")).ServeHTTP(response, adminRequest(http.MethodPost, "/api/v1/admin/order-realizations/prepare", body))
			if response.Code != scenario.status || repo.preparations != 1 || repo.actor.CustomerID != 1 || repo.input.Events[0].ShipmentOfferID != 5 {
				t.Fatalf("status=%d actor=%+v input=%+v: %s", response.Code, repo.actor, repo.input, response.Body.String())
			}
			if scenario.err == nil {
				var result map[string]json.RawMessage
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if _, fabricatedCapability := result["externalWritesEnabled"]; fabricatedCapability || !strings.Contains(string(result["events"]), `"state":"prepared"`) {
					t.Fatalf("internal preparation misrepresented: %s", response.Body.String())
				}
			}
		})
	}
}
