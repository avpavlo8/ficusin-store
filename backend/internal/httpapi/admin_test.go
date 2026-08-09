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

func (stub adminAuthStub) ConfirmCall(context.Context, string, string, auth.Registration, auth.ClientMeta) (string, time.Time, bool, error) {
	return "", time.Time{}, false, nil
}

func (stub adminAuthStub) UpdateProfile(context.Context, int64, auth.Profile) error {
	return nil
}

func (stub adminAuthStub) SaveAvatar(context.Context, int64, []byte, string) error {
	return nil
}

func (stub adminAuthStub) DeleteAvatar(context.Context, int64) error {
	return nil
}

func (stub adminAuthStub) Avatar(context.Context, int64) ([]byte, string, error) {
	return nil, "", nil
}

func (stub adminAuthStub) Logout(context.Context, string) error {
	return nil
}

func (stub adminAuthStub) UserByToken(context.Context, string) (*auth.User, error) {
	return stub.user, nil
}

type adminRepositoryStub struct {
	createdProducts []admin.ProductCreate
	importRequests  []admin.ImportRequest
	updateCustomerCalls int
	syncCalls           int
	createCategoryCalls int
	deliveryFee         float64
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

func (stub *adminRepositoryStub) ListAdminCollections(
	context.Context,
) ([]admin.AdminCollection, error) {
	return []admin.AdminCollection{}, nil
}

func (stub *adminRepositoryStub) SetCollectionProducts(
	context.Context,
	admin.Actor,
	int64,
	[]int64,
) error {
	return nil
}

func (stub *adminRepositoryStub) SetDeliveryFee(
	_ context.Context,
	_ admin.Actor,
	_ int64,
	fee float64,
) (admin.Order, error) {
	stub.deliveryFee = fee
	return admin.Order{}, nil
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

func (stub *adminRepositoryStub) CreateProduct(_ context.Context, _ admin.Actor, input admin.ProductCreate) (admin.Product, error) {
	stub.createdProducts = append(stub.createdProducts, input)
	return admin.Product{ID: 7, Name: input.Name, Slug: "fikus"}, nil
}

func (stub *adminRepositoryStub) ImportProducts(_ context.Context, _ admin.Actor, request admin.ImportRequest) (admin.ImportResult, error) {
	stub.importRequests = append(stub.importRequests, request)
	return admin.ImportResult{Created: 1, Entries: []admin.ImportEntry{{Code: "X1150532", Status: "new"}}}, nil
}

func (stub *adminRepositoryStub) SyncProducts(context.Context, admin.Actor, admin.SyncRequest) (admin.SyncResult, error) {
	stub.syncCalls++
	return admin.SyncResult{Updated: 1, Skipped: []int64{}}, nil
}
func(stub *adminRepositoryStub) ListCategories(context.Context)([]admin.Category,error){return []admin.Category{},nil}
func(stub *adminRepositoryStub) CreateCategory(context.Context,admin.Actor,admin.CategoryCreate)(admin.Category,error){stub.createCategoryCalls++;return admin.Category{ID:1,Name:"Аглаонема",Slug:"aglaonema"},nil}
func(stub *adminRepositoryStub) UpdateCategory(context.Context,admin.Actor,int64,admin.CategoryUpdate)(admin.Category,error){return admin.Category{},nil}
func(stub *adminRepositoryStub) DeleteCategory(context.Context,admin.Actor,int64)error{return nil}

func TestAdminManagerCannotAssignRoles(t *testing.T) {
	t.Parallel()

	repository := &adminRepositoryStub{}
	request := adminRequest(http.MethodPatch, "/api/v1/admin/customers/2", `{"adminRole":"manager"}`)
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), adminDependencies(repository, admin.RoleManager, "manager@example.com")).ServeHTTP(response, request)

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
	NewRouter(discardLogger(), adminDependencies(repository, admin.RoleOwner, "owner@example.com")).ServeHTTP(response, request)

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
	NewRouter(discardLogger(), adminDependencies(repository, admin.RoleOwner, "owner@example.com")).ServeHTTP(response, request)

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
	NewRouter(discardLogger(), adminDependencies(repository, admin.RoleManager, "manager@example.com")).ServeHTTP(response, request)

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
	NewRouter(discardLogger(), adminDependencies(repository, admin.RoleManager, "manager@example.com")).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if repository.syncCalls != 1 {
		t.Fatalf("sync calls = %d, want 1", repository.syncCalls)
	}
}

func TestAdminManagerCanCreateCategory(t *testing.T) {
	t.Parallel()

	repository := &adminRepositoryStub{}
	request := adminRequest(http.MethodPost, "/api/v1/admin/categories", `{"name":"Аглаонема","slug":"aglaonema"}`)
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), adminDependencies(repository, admin.RoleManager, "manager@example.com")).ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusCreated, response.Body.String())
	}
	if repository.createCategoryCalls != 1 {
		t.Fatalf("create category calls = %d, want 1", repository.createCategoryCalls)
	}
}

func TestUnknownRoleCannotReadCustomers(t *testing.T) {
	t.Parallel()

	repository := &adminRepositoryStub{}
	request := adminRequest(http.MethodGet, "/api/v1/admin/customers", "")
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), adminDependencies(repository, "unknown", "unknown@example.com")).ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func adminDependencies(repository adminRepository, role, email string) Dependencies {
	return Dependencies{
		Catalog: catalogStub{},
		Auth:    adminAuthStub{user: &auth.User{ID: 1, Email: email, FullName: "Тест", AdminRole: role}},
		Orders:  orderStub{},
		Admin:   repository,
	}
}

// Admin rights come from admin_users only. Listing an address in
// ADMIN_EMAILS must not by itself open the panel: nothing verifies an
// email, and the account holder can change theirs from the profile page.
func TestAdminEmailAloneGrantsNothing(t *testing.T) {
	t.Parallel()

	repository := &adminRepositoryStub{}
	request := adminRequest(http.MethodGet, "/api/v1/admin/customers", "")
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), adminDependencies(repository, "", "owner@example.com")).ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
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

// Товар заводится в магазине, а не в СБИС: панель должна уметь это без
// всякой связи с чужой системой.
func TestCreateProductRequiresName(t *testing.T) {
	repository := &adminRepositoryStub{}
	router := NewRouter(discardLogger(), adminDependencies(repository, "owner", "owner@example.com"))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/products", strings.NewReader(`{"name":"  "}`))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("ожидали 400, получили %d", recorder.Code)
	}
	if len(repository.createdProducts) != 0 {
		t.Fatal("товар без названия всё-таки завели")
	}
}

func TestCreateProductPasses(t *testing.T) {
	repository := &adminRepositoryStub{}
	router := NewRouter(discardLogger(), adminDependencies(repository, "owner", "owner@example.com"))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/products",
		strings.NewReader(`{"name":"Фикус Бенджамина","priceMinor":149000,"stock":3}`))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("ожидали 201, получили %d", recorder.Code)
	}
	if len(repository.createdProducts) != 1 || repository.createdProducts[0].Name != "Фикус Бенджамина" {
		t.Fatalf("до хранилища дошло не то: %+v", repository.createdProducts)
	}
}

// Пустой список кодов — это опечатка, а не команда «завести всё подряд».
func TestImportRefusesEmptyList(t *testing.T) {
	repository := &adminRepositoryStub{}
	router := NewRouter(discardLogger(), adminDependencies(repository, "owner", "owner@example.com"))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/products/import",
		strings.NewReader(`{"codes":[]}`))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("ожидали 400, получили %d", recorder.Code)
	}
	if len(repository.importRequests) != 0 {
		t.Fatal("пустой импорт всё-таки ушёл в хранилище")
	}
}

func TestImportPassesCodesAndSection(t *testing.T) {
	repository := &adminRepositoryStub{}
	router := NewRouter(discardLogger(), adminDependencies(repository, "owner", "owner@example.com"))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/products/import",
		strings.NewReader(`{"codes":["X1150532","X1150533"],"categoryId":4,"dryRun":true}`))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("ожидали 200, получили %d", recorder.Code)
	}
	if len(repository.importRequests) != 1 {
		t.Fatalf("ожидали один запрос импорта, получили %d", len(repository.importRequests))
	}
	got := repository.importRequests[0]
	if len(got.Codes) != 2 || !got.DryRun || got.CategoryID == nil || *got.CategoryID != 4 {
		t.Fatalf("запрос доехал искажённым: %+v", got)
	}
}
