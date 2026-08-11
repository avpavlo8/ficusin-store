package procurement

import (
	"context"
	"errors"
	"testing"
)

type storeStub struct {
	supplierInput SupplierCreate
	orderInput    OrderCreate
	documentInput DocumentUpload
	parsedInput   ParsedDocument
	searchInput   string
	aliasID       int64
	resolution    AliasResolution
}

func (stub *storeStub) Dashboard(context.Context) (Dashboard, error) { return Dashboard{}, nil }
func (stub *storeStub) CreateSupplier(_ context.Context, _ Actor, input SupplierCreate) (Supplier, error) {
	stub.supplierInput = input
	return Supplier{Name: input.Name, Kind: input.Kind, CountryCode: input.CountryCode, DefaultCurrency: input.DefaultCurrency}, nil
}
func (stub *storeStub) CreateOrder(_ context.Context, _ Actor, input OrderCreate) (OrderSummary, error) {
	stub.orderInput = input
	return OrderSummary{SupplierID: input.SupplierID, SourceKind: input.SourceKind, Currency: input.Currency}, nil
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
