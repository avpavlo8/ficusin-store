package procurement

import (
	"context"
	"errors"
	"testing"
	"time"
)

type storeStub struct {
	supplierInput     SupplierCreate
	orderInput        OrderCreate
	documentInput     DocumentUpload
	parsedInput       ParsedDocument
	searchInput       string
	aliasID           int64
	resolution        AliasResolution
	deletedSupplierID int64
	deletedOrderID    int64
	exclusion         ExclusionUpdate
	batchChannels     []string
	integrationHealth []IntegrationHealth
}

func (stub *storeStub) Dashboard(context.Context) (Dashboard, error) { return Dashboard{}, nil }
func (stub *storeStub) UpdateSettings(_ context.Context, _ Actor, input PricingSettings) (PricingSettings, error) {
	return input, nil
}
func (stub *storeStub) CreateSupplier(_ context.Context, _ Actor, input SupplierCreate) (Supplier, error) {
	stub.supplierInput = input
	return Supplier{Name: input.Name, Kind: input.Kind, CountryCode: input.CountryCode, DefaultCurrency: input.DefaultCurrency}, nil
}
func (stub *storeStub) DeleteSupplier(_ context.Context, _ Actor, supplierID int64) error {
	stub.deletedSupplierID = supplierID
	return nil
}
func (stub *storeStub) CreateOrder(_ context.Context, _ Actor, input OrderCreate) (OrderSummary, error) {
	stub.orderInput = input
	return OrderSummary{SupplierID: input.SupplierID, SourceKind: input.SourceKind, Currency: input.Currency}, nil
}
func (stub *storeStub) CreatePlan(_ context.Context, _ Actor, input PlanCreate) (OrderSummary, error) {
	return OrderSummary{SupplierID: input.SupplierID}, nil
}
func (stub *storeStub) OrderDetail(context.Context, int64) (OrderDetail, error) {
	return OrderDetail{}, nil
}
func (stub *storeStub) CalculateOrder(context.Context, Actor, int64, CalculationInput) (OrderDetail, error) {
	return OrderDetail{}, nil
}
func (stub *storeStub) UpdateOrderStatus(context.Context, Actor, int64, OrderStatusUpdate) (OrderDetail, error) {
	return OrderDetail{}, nil
}
func (stub *storeStub) DeleteOrder(_ context.Context, _ Actor, orderID int64) error {
	stub.deletedOrderID = orderID
	return nil
}
func (stub *storeStub) UpdateOrderLine(context.Context, Actor, int64, OrderLineUpdate) (OrderDetail, error) {
	return OrderDetail{}, nil
}
func (stub *storeStub) ImportDocument(_ context.Context, _ Actor, input DocumentUpload, parsed ParsedDocument) (ImportResult, error) {
	stub.documentInput, stub.parsedInput = input, parsed
	return ImportResult{Document: DocumentSummary{ID: 12}}, nil
}
func (stub *storeStub) SearchNomenclature(_ context.Context, query string) ([]NomenclatureCandidate, error) {
	stub.searchInput = query
	return []NomenclatureCandidate{{SabyID: "X1", Name: "Фикус Лирата"}}, nil
}
func (stub *storeStub) ResolveAlias(_ context.Context, _ Actor, aliasID int64, input AliasResolution) (AliasReview, error) {
	stub.aliasID, stub.resolution = aliasID, input
	return AliasReview{ID: aliasID, MatchStatus: input.MatchStatus, SuggestedSabyID: input.SabyID}, nil
}
func (stub *storeStub) CreateRequest(_ context.Context, _ Actor, input RequestCreate) (Request, error) {
	return Request{Kind: input.Kind}, nil
}
func (stub *storeStub) UpdateRequest(_ context.Context, _ Actor, _ int64, input RequestUpdate) (Request, error) {
	return Request{Status: input.Status}, nil
}
func (stub *storeStub) ListProducts(context.Context, int64, string) ([]ProductDirectoryItem, error) {
	return nil, nil
}
func (stub *storeStub) UpdateProduct(_ context.Context, _ Actor, input ProductDirectoryUpdate) (ProductDirectoryItem, error) {
	return ProductDirectoryItem{SabyID: input.SabyID}, nil
}
func (stub *storeStub) UpdateAvailability(_ context.Context, _ Actor, input AvailabilityUpdate) (AvailabilityItem, error) {
	return AvailabilityItem{SupplierID: input.SupplierID, SabyID: input.SabyID, Status: input.Status}, nil
}
func (stub *storeStub) SetExclusion(_ context.Context, _ Actor, input ExclusionUpdate) error {
	stub.exclusion = input
	return nil
}
func (stub *storeStub) LinkChannelProducts(_ context.Context, _ Actor, channel string, items []ChannelProduct) (ChannelLinkResult, error) {
	return ChannelLinkResult{Channel: channel, Fetched: len(items)}, nil
}
func (stub *storeStub) RememberChannelProducts(context.Context, string, []ChannelProduct) error {
	return nil
}

func (stub *storeStub) PrepareBatch(_ context.Context, _ Actor, _ int64, kind string, channels []string) (ActionBatch, error) {
	stub.batchChannels = append([]string(nil), channels...)
	return ActionBatch{Kind: kind}, nil
}
func (stub *storeStub) ApproveBatch(_ context.Context, _ Actor, batchID int64, _ map[string]bool) (ActionBatch, error) {
	return ActionBatch{ID: batchID}, nil
}
func (stub *storeStub) ClaimAction(context.Context) (*ActionItem, error)                  { return nil, nil }
func (stub *storeStub) FinishAction(context.Context, int64, ActionExecution, error) error { return nil }
func (stub *storeStub) RetryBatch(_ context.Context, _ Actor, batchID int64, _ map[string]bool) (ActionBatch, error) {
	return ActionBatch{ID: batchID}, nil
}
func (stub *storeStub) ListIntegrationHealth(context.Context) ([]IntegrationHealth, error) {
	return stub.integrationHealth, nil
}

type probeExecutorStub struct{ probeCalls int }

func (*probeExecutorStub) Configured(string) bool { return true }
func (*probeExecutorStub) Execute(context.Context, ActionItem) (ActionExecution, error) {
	return ActionExecution{}, nil
}
func (stub *probeExecutorStub) Probe(context.Context, string) error {
	stub.probeCalls++
	return nil
}
func (stub *storeStub) RecordIntegrationCheck(_ context.Context, channel string, configured bool, checkErr error) (IntegrationHealth, error) {
	item := IntegrationHealth{Channel: channel, Configured: configured}
	if checkErr != nil {
		item.LastError = checkErr.Error()
	}
	return item, nil
}

type parserStub struct {
	result ParsedDocument
	err    error
}

func (stub parserStub) Parse([]byte) (ParsedDocument, error) { return stub.result, stub.err }

func TestCreateSupplierNormalizesInput(t *testing.T) {
	t.Parallel()
	store := &storeStub{}
	_, err := NewService(store).CreateSupplier(context.Background(), Actor{}, SupplierCreate{
		Name: "  ТК Ярославский ", Kind: KindDomestic, CountryCode: "ru", DefaultCurrency: "rub",
		TaxID: "7627031650", KPP: "762701001",
	})
	if err != nil {
		t.Fatal(err)
	}
	if store.supplierInput.Name != "ТК Ярославский" || store.supplierInput.CountryCode != "RU" || store.supplierInput.DefaultCurrency != "RUB" {
		t.Fatalf("unexpected normalized supplier: %+v", store.supplierInput)
	}
}

func TestPreparePricesKeepsOnlySelectedUniqueChannels(t *testing.T) {
	t.Parallel()
	store := &storeStub{}
	service := NewServiceWithExecutor(store, channelCatalogStub{})
	_, err := service.PrepareBatch(context.Background(), Actor{}, 12, "prices", []string{"saby_price", "ozon", "ozon", "unknown"})
	if err != nil {
		t.Fatal(err)
	}
	if len(store.batchChannels) != 2 || store.batchChannels[0] != "saby_price" || store.batchChannels[1] != "ozon" {
		t.Fatalf("channels = %#v", store.batchChannels)
	}
}

func TestWildberriesConnectionCheckReadsMirrorWithoutCallingAPI(t *testing.T) {
	t.Parallel()
	moment := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	store := &storeStub{integrationHealth: []IntegrationHealth{{Channel: "wb", LastSuccessAt: &moment}}}
	executor := &probeExecutorStub{}
	result, err := NewServiceWithExecutor(store, executor).CheckIntegration(context.Background(), Actor{}, "wb")
	if err != nil || result.LastError != "" {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	if executor.probeCalls != 0 {
		t.Fatalf("WB probe calls = %d, want 0", executor.probeCalls)
	}
}

func TestDeleteSupplierValidatesID(t *testing.T) {
	t.Parallel()
	store := &storeStub{}
	service := NewService(store)
	if err := service.DeleteSupplier(context.Background(), Actor{}, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
	if err := service.DeleteSupplier(context.Background(), Actor{}, 17); err != nil || store.deletedSupplierID != 17 {
		t.Fatalf("deleted supplier = %d, err = %v", store.deletedSupplierID, err)
	}
}

func TestDeleteOrderValidatesID(t *testing.T) {
	store := &storeStub{}
	service := NewService(store)
	if err := service.DeleteOrder(context.Background(), Actor{}, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v", err)
	}
	if err := service.DeleteOrder(context.Background(), Actor{}, 23); err != nil || store.deletedOrderID != 23 {
		t.Fatalf("error = %v, deleted = %d", err, store.deletedOrderID)
	}
}

func TestCreateSupplierRejectsUnknownCurrency(t *testing.T) {
	t.Parallel()
	_, err := NewService(&storeStub{}).CreateSupplier(context.Background(), Actor{}, SupplierCreate{
		Name: "Поставщик", Kind: KindDomestic, DefaultCurrency: "CNY",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
}

func TestCreateOrderRequiresSupplier(t *testing.T) {
	t.Parallel()
	_, err := NewService(&storeStub{}).CreateOrder(context.Background(), Actor{}, OrderCreate{
		SourceKind: SourceManual, Currency: "EUR",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
}

func TestImportDocumentValidatesAndDelegatesParsedPDF(t *testing.T) {
	t.Parallel()
	store := &storeStub{}
	parsed := ParsedDocument{ParserKind: "domestic_payment_invoice", Lines: []ParsedLine{{RawName: "Фикус"}}}
	result, err := NewServiceWithParser(store, parserStub{result: parsed}).ImportDocument(
		context.Background(), Actor{}, DocumentUpload{
			SupplierID: 3, FileName: " invoice.pdf ", ContentType: "application/pdf", Content: []byte("%PDF-test"),
		})
	if err != nil {
		t.Fatal(err)
	}
	if result.Document.ID != 12 || store.documentInput.FileName != "invoice.pdf" || store.parsedInput.ParserKind != parsed.ParserKind {
		t.Fatalf("unexpected import: result=%+v input=%+v parsed=%+v", result, store.documentInput, store.parsedInput)
	}
}

func TestImportDocumentRejectsNonPDF(t *testing.T) {
	t.Parallel()
	_, err := NewServiceWithParser(&storeStub{}, parserStub{}).ImportDocument(
		context.Background(), Actor{}, DocumentUpload{SupplierID: 1, FileName: "invoice.pdf", Content: []byte("hello")},
	)
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
}

func TestSearchNomenclatureAndResolveAliasValidateInput(t *testing.T) {
	t.Parallel()
	store := &storeStub{}
	service := NewService(store)
	items, err := service.SearchNomenclature(context.Background(), "  Фикус ")
	if err != nil || len(items) != 1 || store.searchInput != "Фикус" {
		t.Fatalf("unexpected search: items=%+v input=%q err=%v", items, store.searchInput, err)
	}
	item, err := service.ResolveAlias(context.Background(), Actor{}, 17, AliasResolution{MatchStatus: "confirmed", SabyID: " X1 "})
	if err != nil || item.ID != 17 || store.resolution.SabyID != "X1" {
		t.Fatalf("unexpected resolution: item=%+v input=%+v err=%v", item, store.resolution, err)
	}
	if _, err := service.ResolveAlias(context.Background(), Actor{}, 17, AliasResolution{MatchStatus: "ignored", SabyID: "X1"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
}
