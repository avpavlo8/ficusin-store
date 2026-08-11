package procurement

import (
	"context"
	"strings"
	"time"
)

type Store interface {
	Dashboard(context.Context) (Dashboard, error)
	UpdateSettings(context.Context, Actor, PricingSettings) (PricingSettings, error)
	CreateSupplier(context.Context, Actor, SupplierCreate) (Supplier, error)
	CreateOrder(context.Context, Actor, OrderCreate) (OrderSummary, error)
	CreatePlan(context.Context, Actor, PlanCreate) (OrderSummary, error)
	OrderDetail(context.Context, int64) (OrderDetail, error)
	CalculateOrder(context.Context, Actor, int64, CalculationInput) (OrderDetail, error)
	ImportDocument(context.Context, Actor, DocumentUpload, ParsedDocument) (ImportResult, error)
	SearchNomenclature(context.Context, string) ([]NomenclatureCandidate, error)
	ResolveAlias(context.Context, Actor, int64, AliasResolution) (AliasReview, error)
	CreateRequest(context.Context, Actor, RequestCreate) (Request, error)
	UpdateAvailability(context.Context, Actor, int64, AvailabilityUpdate) (AliasReview, error)
	PrepareBatch(context.Context, Actor, int64, string) (ActionBatch, error)
	ApproveBatch(context.Context, Actor, int64) (ActionBatch, error)
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

func (service *Service) UpdateSettings(ctx context.Context, actor Actor, input PricingSettings) (PricingSettings, error) {
	if input.DefaultExchangeRate <= 0 || input.TrolleyCostCurrency < 0 || input.TrolleyVolumeCM3 <= 0 ||
		input.TrolleyFillRatio <= 0 || input.TrolleyFillRatio > 1 || input.PackageRUB < 0 ||
		input.PriceChangeThreshold < 0 || input.PriceChangeThreshold >= 1 || input.RetailRoundStep <= 0 ||
		input.RecommendationDays < 7 || input.RecommendationDays > 365 || input.TargetCoverDays < 1 || input.TargetCoverDays > 365 ||
		!validRate(input.ReturnLossRate) || !validRate(input.MarketplaceCostRate) || !validRate(input.TaxRate) ||
		!validRate(input.ReserveRate) || !validRate(input.MarketplaceStrikeMarkup) ||
		input.DomesticRetailMultiplier <= 0 || input.InternationalCostMultiplier <= 0 || input.InternationalRetailMultiplier <= 0 {
		return PricingSettings{}, ErrInvalidInput
	}
	return service.store.UpdateSettings(ctx, actor, input)
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

func (service *Service) CreatePlan(ctx context.Context, actor Actor, input PlanCreate) (OrderSummary, error) {
	input.OrderNumber = strings.TrimSpace(input.OrderNumber)
	if input.SupplierID <= 0 || len(input.Items) == 0 || len(input.Items) > 500 {
		return OrderSummary{}, ErrInvalidInput
	}
	seen := make(map[string]bool, len(input.Items))
	for index := range input.Items {
		input.Items[index].SabyID = strings.TrimSpace(input.Items[index].SabyID)
		item := input.Items[index]
		if item.SabyID == "" || item.Quantity <= 0 || item.ExpectedUnitPrice < 0 || seen[item.SabyID] {
			return OrderSummary{}, ErrInvalidInput
		}
		seen[item.SabyID] = true
	}
	return service.store.CreatePlan(ctx, actor, input)
}

func (service *Service) OrderDetail(ctx context.Context, orderID int64) (OrderDetail, error) {
	if orderID <= 0 {
		return OrderDetail{}, ErrInvalidInput
	}
	return service.store.OrderDetail(ctx, orderID)
}

func (service *Service) CalculateOrder(ctx context.Context, actor Actor, orderID int64, input CalculationInput) (OrderDetail, error) {
	if orderID <= 0 || input.ExchangeRate <= 0 || input.TrolleyCostCurrency < 0 || input.DeliveryToRyazanRUB < 0 {
		return OrderDetail{}, ErrInvalidInput
	}
	return service.store.CalculateOrder(ctx, actor, orderID, input)
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

func (service *Service) CreateRequest(ctx context.Context, actor Actor, input RequestCreate) (Request, error) {
	input.Kind = strings.TrimSpace(input.Kind)
	input.SabyID = strings.TrimSpace(input.SabyID)
	input.RequestedName = strings.TrimSpace(input.RequestedName)
	input.Notes = strings.TrimSpace(input.Notes)
	if !oneOf(input.Kind, "customer_order", "staff_recommendation") || input.RequestedName == "" || input.Quantity <= 0 {
		return Request{}, ErrInvalidInput
	}
	return service.store.CreateRequest(ctx, actor, input)
}

func (service *Service) UpdateAvailability(ctx context.Context, actor Actor, aliasID int64, input AvailabilityUpdate) (AliasReview, error) {
	input.Status = strings.TrimSpace(input.Status)
	input.CheckAfter = strings.TrimSpace(input.CheckAfter)
	if aliasID <= 0 || !oneOf(input.Status, "available", "unknown", "check", "temporarily_unavailable", "discontinued") {
		return AliasReview{}, ErrInvalidInput
	}
	if input.CheckAfter != "" {
		if _, err := time.Parse("2006-01-02", input.CheckAfter); err != nil {
			return AliasReview{}, ErrInvalidInput
		}
	}
	return service.store.UpdateAvailability(ctx, actor, aliasID, input)
}

func (service *Service) PrepareBatch(ctx context.Context, actor Actor, orderID int64, kind string) (ActionBatch, error) {
	kind = strings.TrimSpace(kind)
	if orderID <= 0 || !oneOf(kind, "receipt", "prices") {
		return ActionBatch{}, ErrInvalidInput
	}
	return service.store.PrepareBatch(ctx, actor, orderID, kind)
}

func (service *Service) ApproveBatch(ctx context.Context, actor Actor, batchID int64) (ActionBatch, error) {
	if batchID <= 0 {
		return ActionBatch{}, ErrInvalidInput
	}
	return service.store.ApproveBatch(ctx, actor, batchID)
}

func validRate(value float64) bool { return value >= 0 && value < 1 }

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
