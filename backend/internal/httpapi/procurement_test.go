package httpapi

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/procurement"
)

type procurementStub struct {
	dashboard          procurement.Dashboard
	supplierInputs     []procurement.SupplierCreate
	orderInputs        []procurement.OrderCreate
	documentInputs     []procurement.DocumentUpload
	searchInputs       []string
	aliasInputs        []procurement.AliasResolution
	deletedSupplierIDs []int64
	deletedOrderIDs    []int64
}

func (stub *procurementStub) Dashboard(context.Context) (procurement.Dashboard, error) {
	return stub.dashboard, nil
}
func (stub *procurementStub) UpdateSettings(_ context.Context, _ procurement.Actor, input procurement.PricingSettings) (procurement.PricingSettings, error) {
	return input, nil
}

func (stub *procurementStub) CreateSupplier(_ context.Context, _ procurement.Actor, input procurement.SupplierCreate) (procurement.Supplier, error) {
	stub.supplierInputs = append(stub.supplierInputs, input)
	return procurement.Supplier{ID: 7, Name: input.Name}, nil
}
func (stub *procurementStub) DeleteSupplier(_ context.Context, _ procurement.Actor, supplierID int64) error {
	stub.deletedSupplierIDs = append(stub.deletedSupplierIDs, supplierID)
	return nil
}

func (stub *procurementStub) CreateOrder(_ context.Context, _ procurement.Actor, input procurement.OrderCreate) (procurement.OrderSummary, error) {
	stub.orderInputs = append(stub.orderInputs, input)
	return procurement.OrderSummary{ID: 9, SupplierID: input.SupplierID}, nil
}
func (stub *procurementStub) CreatePlan(_ context.Context, _ procurement.Actor, input procurement.PlanCreate) (procurement.OrderSummary, error) {
	return procurement.OrderSummary{ID: 10, SupplierID: input.SupplierID}, nil
}
func (stub *procurementStub) OrderDetail(context.Context, int64) (procurement.OrderDetail, error) {
	return procurement.OrderDetail{}, nil
}
func (stub *procurementStub) CalculateOrder(_ context.Context, _ procurement.Actor, _ int64, _ procurement.CalculationInput) (procurement.OrderDetail, error) {
	return procurement.OrderDetail{}, nil
}
func (stub *procurementStub) UpdateOrderStatus(context.Context, procurement.Actor, int64, procurement.OrderStatusUpdate) (procurement.OrderDetail, error) {
	return procurement.OrderDetail{}, nil
}
func (stub *procurementStub) DeleteOrder(_ context.Context, _ procurement.Actor, orderID int64) error {
	stub.deletedOrderIDs = append(stub.deletedOrderIDs, orderID)
	return nil
}
func (stub *procurementStub) UpdateOrderLine(context.Context, procurement.Actor, int64, procurement.OrderLineUpdate) (procurement.OrderDetail, error) {
	return procurement.OrderDetail{}, nil
}

func (stub *procurementStub) ImportDocument(_ context.Context, _ procurement.Actor, input procurement.DocumentUpload) (procurement.ImportResult, error) {
	stub.documentInputs = append(stub.documentInputs, input)
	return procurement.ImportResult{Document: procurement.DocumentSummary{ID: 12}}, nil
}
func (stub *procurementStub) SearchNomenclature(_ context.Context, query string) ([]procurement.NomenclatureCandidate, error) {
	stub.searchInputs = append(stub.searchInputs, query)
	return []procurement.NomenclatureCandidate{{SabyID: "X1", Name: "Фикус Лирата"}}, nil
}
func (stub *procurementStub) ResolveAlias(_ context.Context, _ procurement.Actor, aliasID int64, input procurement.AliasResolution) (procurement.AliasReview, error) {
	stub.aliasInputs = append(stub.aliasInputs, input)
	return procurement.AliasReview{ID: aliasID, MatchStatus: input.MatchStatus}, nil
}
func (stub *procurementStub) CreateRequest(_ context.Context, _ procurement.Actor, input procurement.RequestCreate) (procurement.Request, error) {
	return procurement.Request{ID: 1, Kind: input.Kind}, nil
}
func (stub *procurementStub) UpdateRequest(_ context.Context, _ procurement.Actor, _ int64, input procurement.RequestUpdate) (procurement.Request, error) {
	return procurement.Request{Status: input.Status}, nil
}
func (stub *procurementStub) ListProducts(context.Context, int64, string) ([]procurement.ProductDirectoryItem, error) {
	return nil, nil
}
func (stub *procurementStub) UpdateProduct(_ context.Context, _ procurement.Actor, input procurement.ProductDirectoryUpdate) (procurement.ProductDirectoryItem, error) {
	return procurement.ProductDirectoryItem{SabyID: input.SabyID}, nil
}
func (stub *procurementStub) UpdateAvailability(_ context.Context, _ procurement.Actor, input procurement.AvailabilityUpdate) (procurement.AvailabilityItem, error) {
	return procurement.AvailabilityItem{SupplierID: input.SupplierID, SabyID: input.SabyID, Status: input.Status}, nil
}
func (stub *procurementStub) SetExclusion(context.Context, procurement.Actor, procurement.ExclusionUpdate) error {
	return nil
}
func (stub *procurementStub) SyncChannelCatalog(_ context.Context, _ procurement.Actor, channel string) (procurement.ChannelLinkResult, error) {
	return procurement.ChannelLinkResult{Channel: channel}, nil
}
func (stub *procurementStub) PrepareBatch(_ context.Context, _ procurement.Actor, _ int64, kind string, _ []string) (procurement.ActionBatch, error) {
	return procurement.ActionBatch{ID: 1, Kind: kind}, nil
}
func (stub *procurementStub) ApproveBatch(_ context.Context, _ procurement.Actor, batchID int64) (procurement.ActionBatch, error) {
	return procurement.ActionBatch{ID: batchID, Status: "completed"}, nil
}
func (stub *procurementStub) RetryBatch(_ context.Context, _ procurement.Actor, batchID int64) (procurement.ActionBatch, error) {
	return procurement.ActionBatch{ID: batchID, Status: "processing"}, nil
}
func (stub *procurementStub) CheckIntegration(_ context.Context, _ procurement.Actor, channel string) (procurement.IntegrationHealth, error) {
	return procurement.IntegrationHealth{Channel: channel, Configured: true}, nil
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

func TestProcurementSupplierDeletion(t *testing.T) {
	t.Parallel()
	service := &procurementStub{}
	request := adminRequest(http.MethodDelete, "/api/v1/admin/procurement/suppliers/7", "")
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), procurementDependencies(service, admin.RoleManager)).ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusNoContent, response.Body.String())
	}
	if len(service.deletedSupplierIDs) != 1 || service.deletedSupplierIDs[0] != 7 {
		t.Fatalf("unexpected calls: %+v", service.deletedSupplierIDs)
	}
}

func TestProcurementDocumentImportAcceptsMultipartPDF(t *testing.T) {
	t.Parallel()
	service := &procurementStub{}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("supplierId", "7")
	_ = writer.WriteField("orderId", "9")
	file, err := writer.CreateFormFile("file", "invoice.pdf")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte("%PDF-test"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := adminRequest(http.MethodPost, "/api/v1/admin/procurement/documents", "")
	request.Body = io.NopCloser(bytes.NewReader(body.Bytes()))
	request.ContentLength = int64(body.Len())
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), procurementDependencies(service, admin.RoleManager)).ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusCreated, response.Body.String())
	}
	if len(service.documentInputs) != 1 || service.documentInputs[0].SupplierID != 7 || service.documentInputs[0].OrderID != 9 {
		t.Fatalf("unexpected calls: %+v", service.documentInputs)
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

func TestProcurementNomenclatureSearchAndAliasResolution(t *testing.T) {
	t.Parallel()
	service := &procurementStub{}
	router := NewRouter(discardLogger(), procurementDependencies(service, admin.RoleManager))

	search := adminRequest(http.MethodGet, "/api/v1/admin/procurement/nomenclature?q=Фикус", "")
	searchResponse := httptest.NewRecorder()
	router.ServeHTTP(searchResponse, search)
	if searchResponse.Code != http.StatusOK || len(service.searchInputs) != 1 || service.searchInputs[0] != "Фикус" {
		t.Fatalf("unexpected search response: status=%d calls=%+v body=%s", searchResponse.Code, service.searchInputs, searchResponse.Body.String())
	}

	resolve := adminRequest(http.MethodPatch, "/api/v1/admin/procurement/aliases/9", `{"matchStatus":"confirmed","sabyId":"X1"}`)
	resolveResponse := httptest.NewRecorder()
	router.ServeHTTP(resolveResponse, resolve)
	if resolveResponse.Code != http.StatusOK || len(service.aliasInputs) != 1 || service.aliasInputs[0].SabyID != "X1" {
		t.Fatalf("unexpected resolution response: status=%d calls=%+v body=%s", resolveResponse.Code, service.aliasInputs, resolveResponse.Body.String())
	}
}
