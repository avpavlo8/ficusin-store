package procurement

import (
	"context"
	"strings"
)

type Store interface {
	Dashboard(context.Context) (Dashboard, error)
	CreateSupplier(context.Context, Actor, SupplierCreate) (Supplier, error)
	CreateOrder(context.Context, Actor, OrderCreate) (OrderSummary, error)
	ImportDocument(context.Context, Actor, DocumentUpload, ParsedDocument) (ImportResult, error)
	SearchNomenclature(context.Context, string) ([]NomenclatureCandidate, error)
	ResolveAlias(context.Context, Actor, int64, AliasResolution) (AliasReview, error)
}

type Service struct {
	store  Store
	parser Parser
}

func NewService(store Store) *Service {
	return &Service{store: store, parser: NewPDFParser()}
}

func NewServiceWithParser(store Store, parser Parser) *Service {
	return &Service{store: store, parser: parser}
}

func (service *Service) Dashboard(ctx context.Context) (Dashboard, error) {
	return service.store.Dashboard(ctx)
}

func (service *Service) CreateSupplier(
	ctx context.Context,
	actor Actor,
	input SupplierCreate,
) (Supplier, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Kind = strings.TrimSpace(input.Kind)
	input.CountryCode = strings.ToUpper(strings.TrimSpace(input.CountryCode))
	input.DefaultCurrency = strings.ToUpper(strings.TrimSpace(input.DefaultCurrency))
	if input.Name == "" || !oneOf(input.Kind, KindInternational, KindDomestic) ||
		!oneOf(input.DefaultCurrency, "EUR", "USD", "RUB") || len(input.CountryCode) > 2 {
		return Supplier{}, ErrInvalidInput
	}
	return service.store.CreateSupplier(ctx, actor, input)
}

func (service *Service) CreateOrder(
	ctx context.Context,
	actor Actor,
	input OrderCreate,
) (OrderSummary, error) {
	input.OrderNumber = strings.TrimSpace(input.OrderNumber)
	input.SourceKind = strings.TrimSpace(input.SourceKind)
	input.Currency = strings.ToUpper(strings.TrimSpace(input.Currency))
	input.Notes = strings.TrimSpace(input.Notes)
	if input.SupplierID <= 0 || !oneOf(input.SourceKind,
		SourceRecommendation, SourceManual, SourceInvoice, SourcePaymentInvoice,
	) || !oneOf(input.Currency, "EUR", "USD", "RUB") {
		return OrderSummary{}, ErrInvalidInput
	}
	return service.store.CreateOrder(ctx, actor, input)
}

func (service *Service) ImportDocument(
	ctx context.Context,
	actor Actor,
	input DocumentUpload,
) (ImportResult, error) {
	input.FileName = strings.TrimSpace(input.FileName)
	input.ContentType = strings.ToLower(strings.TrimSpace(input.ContentType))
	if separator := strings.IndexByte(input.ContentType, ';'); separator >= 0 {
		input.ContentType = strings.TrimSpace(input.ContentType[:separator])
	}
	if input.SupplierID <= 0 || input.OrderID < 0 || input.FileName == "" ||
		len(input.Content) < 5 || len(input.Content) > 20<<20 ||
		!strings.HasPrefix(string(input.Content[:5]), "%PDF-") ||
		!oneOf(input.ContentType, "application/pdf", "application/octet-stream", "") {
		return ImportResult{}, ErrInvalidInput
	}
	parsed, err := service.parser.Parse(input.Content)
	if err != nil {
		return ImportResult{}, err
	}
	return service.store.ImportDocument(ctx, actor, input, parsed)
}

func (service *Service) SearchNomenclature(ctx context.Context, query string) ([]NomenclatureCandidate, error) {
	query = strings.TrimSpace(query)
	if len([]rune(query)) < 2 || len(query) > 200 {
		return nil, ErrInvalidInput
	}
	return service.store.SearchNomenclature(ctx, query)
}

func (service *Service) ResolveAlias(
	ctx context.Context,
	actor Actor,
	aliasID int64,
	input AliasResolution,
) (AliasReview, error) {
	input.MatchStatus = strings.TrimSpace(input.MatchStatus)
	input.SabyID = strings.TrimSpace(input.SabyID)
	if aliasID <= 0 || !oneOf(input.MatchStatus, "confirmed", "new_product", "ignored") ||
		(input.MatchStatus == "confirmed" && input.SabyID == "") ||
		(input.MatchStatus != "confirmed" && input.SabyID != "") {
		return AliasReview{}, ErrInvalidInput
	}
	return service.store.ResolveAlias(ctx, actor, aliasID, input)
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
