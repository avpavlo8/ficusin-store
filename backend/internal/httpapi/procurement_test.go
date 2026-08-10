package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/procurement"
)

type procurementStub struct {
	dashboard      procurement.Dashboard
	supplierInputs []procurement.SupplierCreate
	orderInputs    []procurement.OrderCreate
}

func (stub *procurementStub) Dashboard(context.Context) (procurement.Dashboard, error) {
	return stub.dashboard, nil
}

func (stub *procurementStub) CreateSupplier(_ context.Context, _ procurement.Actor, input procurement.SupplierCreate) (procurement.Supplier, error) {
	stub.supplierInputs = append(stub.supplierInputs, input)
	return procurement.Supplier{ID: 7, Name: input.Name}, nil
}

func (stub *procurementStub) CreateOrder(_ context.Context, _ procurement.Actor, input procurement.OrderCreate) (procurement.OrderSummary, error) {
	stub.orderInputs = append(stub.orderInputs, input)
	return procurement.OrderSummary{ID: 9, SupplierID: input.SupplierID}, nil
}

func procurementDependencies(service procurementService, role string) Dependencies {
	dependencies := adminDependencies(&adminRepositoryStub{}, role, "manager@example.com")
	dependencies.Procurement = service
	return dependencies
}

func TestProcurementDashboardAvailableToManager(t *testing.T) {
	t.Parallel()
	service := &procurementStub{dashboard: procurement.Dashboard{
		Summary: procurement.Summary{OpenOrders: 2, UnresolvedAliases: 11},
	}}
	request := adminRequest(http.MethodGet, "/api/v1/admin/procurement", "")
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), procurementDependencies(service, admin.RoleManager)).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if !strings.Contains(response.Body.String(), `"unresolvedAliases":11`) {
		t.Fatalf("unexpected body: %s", response.Body.String())
	}
}

func TestProcurementSupplierCreation(t *testing.T) {
	t.Parallel()
	service := &procurementStub{}
	request := adminRequest(http.MethodPost, "/api/v1/admin/procurement/suppliers", `{"name":"ТК Ярославский","kind":"domestic","countryCode":"RU","defaultCurrency":"RUB"}`)
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), procurementDependencies(service, admin.RoleManager)).ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusCreated, response.Body.String())
	}
	if len(service.supplierInputs) != 1 || service.supplierInputs[0].DefaultCurrency != "RUB" {
		t.Fatalf("unexpected calls: %+v", service.supplierInputs)
	}
}

func TestProcurementRejectsUnknownRole(t *testing.T) {
	t.Parallel()
	request := adminRequest(http.MethodGet, "/api/v1/admin/procurement", "")
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), procurementDependencies(&procurementStub{}, "unknown")).ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}
