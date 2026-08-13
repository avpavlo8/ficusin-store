package procurement

import (
	"context"
	"errors"
	"testing"
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
	exclusion         ExclusionUpdate
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
func (stub *storeStub) PrepareBatch(_ context.Context, _ Actor, _ int64, kind string) (ActionBatch, error) {
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
	return nil, nil
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
	})
	if err != nil {
		t.Fatal(err)
	}
	if store.supplierInput.Name != "ТК Ярославский" || store.supplierInput.CountryCode != "RU" || store.supplierInput.DefaultCurrency != "RUB" {
		t.Fatalf("unexpected normalized supplier: %+v", store.supplierInput)
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
