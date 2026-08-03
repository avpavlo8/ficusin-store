package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/auth"
)

type adminAuthStub struct {
	user *auth.User
}

func (stub adminAuthStub) RequestCall(context.Context, string) (string, string, string, error) {
	return "", "", "", nil
}

func (stub adminAuthStub) ConfirmCall(context.Context, string, string, auth.Registration, string) (string, time.Time, bool, error) {
	return "", time.Time{}, false, nil
}

func (stub adminAuthStub) Logout(context.Context, string) error {
	return nil
}

func (stub adminAuthStub) UserByToken(context.Context, string) (*auth.User, error) {
	return stub.user, nil
}

type adminRepositoryStub struct {
	updateCustomerCalls int
	syncCalls           int
}

func (stub *adminRepositoryStub) Dashboard(context.Context) (admin.Dashboard, error) {
	return admin.Dashboard{}, nil
}

func (stub *adminRepositoryStub) ListCustomers(context.Context) ([]admin.Customer, error) {
	return []admin.Customer{}, nil
}

func (stub *adminRepositoryStub) UpdateCustomer(context.Context, admin.Actor, int64, admin.CustomerUpdate) (admin.Customer, error) {
	stub.updateCustomerCalls++
	return admin.Customer{ID: 2}, nil
}

func (stub *adminRepositoryStub) ListOrders(context.Context) ([]admin.Order, error) {
	return []admin.Order{}, nil
}

func (stub *adminRepositoryStub) UpdateOrderStatus(context.Context, admin.Actor, int64, string, string) (admin.Order, error) {
	return admin.Order{}, nil
}

func (stub *adminRepositoryStub) ListProducts(context.Context) ([]admin.Product, error) {
	return []admin.Product{}, nil
}

func (stub *adminRepositoryStub) UpdateProduct(context.Context, admin.Actor, int64, admin.ProductUpdate) (admin.Product, error) {
	return admin.Product{}, nil
}

func (stub *adminRepositoryStub) SyncProducts(context.Context, admin.Actor, admin.SyncRequest) (admin.SyncResult, error) {
	stub.syncCalls++
	return admin.SyncResult{Updated: 1, Skipped: []int64{}}, nil
}

func TestAdminManagerCannotAssignRoles(t *testing.T) {
	t.Parallel()

	repository := &adminRepositoryStub{}
	request := adminRequest(http.MethodPatch, "/api/v1/admin/customers/2", `{"adminRole":"manager"}`)
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), adminDependencies(repository, admin.RoleManager, "manager@example.com", nil)).ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
	if repository.updateCustomerCalls != 0 {
		t.Fatal("repository must not be called for forbidden role assignment")
	}
}

func TestAdminOwnerCanAssignRoles(t *testing.T) {
	t.Parallel()

	repository := &adminRepositoryStub{}
	request := adminRequest(http.MethodPatch, "/api/v1/admin/customers/2", `{"adminRole":"manager"}`)
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), adminDependencies(repository, "", "owner@example.com", []string{"owner@example.com"})).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if repository.updateCustomerCalls != 1 {
		t.Fatalf("update calls = %d, want 1", repository.updateCustomerCalls)
	}
}

func TestAdminOwnerCannotCreateAnotherOwner(t *testing.T) {
	t.Parallel()

	repository := &adminRepositoryStub{}
	request := adminRequest(http.MethodPatch, "/api/v1/admin/customers/2", `{"adminRole":"owner"}`)
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), adminDependencies(repository, "", "owner@example.com", []string{"owner@example.com"})).ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
	if repository.updateCustomerCalls != 0 {
		t.Fatal("repository must not be called for owner role assignment")
	}
}

func TestAdminManagerCanBulkSync(t *testing.T) {
	t.Parallel()

	repository := &adminRepositoryStub{}
	request := adminRequest(http.MethodPost, "/api/v1/admin/products/sync", `{"productIds":[1,2],"fields":["price"]}`)
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), adminDependencies(repository, admin.RoleManager, "manager@example.com", nil)).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if repository.syncCalls != 1 {
		t.Fatalf("sync calls = %d, want 1", repository.syncCalls)
	}
}

func TestAdminManagerCanSyncOneProduct(t *testing.T) {
	t.Parallel()

	repository := &adminRepositoryStub{}
	request := adminRequest(http.MethodPost, "/api/v1/admin/products/sync", `{"productIds":[1],"fields":["price"]}`)
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), adminDependencies(repository, admin.RoleManager, "manager@example.com", nil)).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if repository.syncCalls != 1 {
		t.Fatalf("sync calls = %d, want 1", repository.syncCalls)
	}
}

func TestUnknownRoleCannotReadCustomers(t *testing.T) {
	t.Parallel()

	repository := &adminRepositoryStub{}
	request := adminRequest(http.MethodGet, "/api/v1/admin/customers", "")
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), adminDependencies(repository, "unknown", "unknown@example.com", nil)).ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func adminDependencies(repository adminRepository, role, email string, ownerEmails []string) Dependencies {
	return Dependencies{
		Catalog:     catalogStub{},
		Auth:        adminAuthStub{user: &auth.User{ID: 1, Email: email, FullName: "Тест", AdminRole: role}},
		Orders:      orderStub{},
		Admin:       repository,
		AdminEmails: ownerEmails,
	}
}

func adminRequest(method, target, body string) *http.Request {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "session"})
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	return request
}
