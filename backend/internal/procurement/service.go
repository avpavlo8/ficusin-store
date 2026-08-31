package procurement

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Store interface {
	Dashboard(context.Context) (Dashboard, error)
	UpdateSettings(context.Context, Actor, PricingSettings) (PricingSettings, error)
	CreateSupplier(context.Context, Actor, SupplierCreate) (Supplier, error)
	DeleteSupplier(context.Context, Actor, int64) error
	CreateOrder(context.Context, Actor, OrderCreate) (OrderSummary, error)
	CreatePlan(context.Context, Actor, PlanCreate) (OrderSummary, error)
	OrderDetail(context.Context, int64) (OrderDetail, error)
	CalculateOrder(context.Context, Actor, int64, CalculationInput) (OrderDetail, error)
	UpdateOrderStatus(context.Context, Actor, int64, OrderStatusUpdate) (OrderDetail, error)
	DeleteOrder(context.Context, Actor, int64) error
	UpdateOrderLine(context.Context, Actor, int64, OrderLineUpdate) (OrderDetail, error)
	ImportDocument(context.Context, Actor, DocumentUpload, ParsedDocument) (ImportResult, error)
	SearchNomenclature(context.Context, string) ([]NomenclatureCandidate, error)
	ResolveAlias(context.Context, Actor, int64, AliasResolution) (AliasReview, error)
	CreateRequest(context.Context, Actor, RequestCreate) (Request, error)
	UpdateRequest(context.Context, Actor, int64, RequestUpdate) (Request, error)
	UpdateAvailability(context.Context, Actor, AvailabilityUpdate) (AvailabilityItem, error)
	SetExclusion(context.Context, Actor, ExclusionUpdate) error
	LinkChannelProducts(context.Context, Actor, string, []ChannelProduct) (ChannelLinkResult, error)
	ListProducts(context.Context, int64, string) ([]ProductDirectoryItem, error)
	UpdateProduct(context.Context, Actor, ProductDirectoryUpdate) (ProductDirectoryItem, error)
	PrepareBatch(context.Context, Actor, int64, string, []string) (ActionBatch, error)
	ApproveBatch(context.Context, Actor, int64, map[string]bool) (ActionBatch, error)
	ClaimAction(context.Context) (*ActionItem, error)
	FinishAction(context.Context, int64, ActionExecution, error) error
	RetryBatch(context.Context, Actor, int64, map[string]bool) (ActionBatch, error)
	ListIntegrationHealth(context.Context) ([]IntegrationHealth, error)
	RecordIntegrationCheck(context.Context, string, bool, error) (IntegrationHealth, error)
}

type Executor interface {
	Configured(channel string) bool
	Execute(context.Context, ActionItem) (ActionExecution, error)
}

// ActionGroupStore and GroupExecutor are optional bulk extensions used by
// marketplace adapters. Both Wildberries and Ozon accept up to 1000 prices in
// one request; claiming them together avoids seller-wide rate limits while the
// base Store/Executor interfaces stay compatible with lightweight test stubs.
type ActionGroupStore interface {
	ClaimActionGroup(context.Context) ([]ActionItem, error)
}

type ActionOutcome struct {
	ItemID int64
	Result ActionExecution
	Err    error
}

type GroupExecutor interface {
	ExecuteGroup(context.Context, []ActionItem) []ActionOutcome
}

type IntegrationProber interface {
	Probe(context.Context, string) error
}

// ChannelCatalogSource читает справочник карточек маркетплейса.
type ChannelCatalogSource interface {
	FetchCatalog(context.Context, string) ([]ChannelProduct, error)
}

// SabyCatalogRefresher performs an on-demand import of the complete Saby
// nomenclature into the local directory. The implementation lives in the
// integration package; procurement only coordinates the operator action.
type SabyCatalogRefresher interface {
	RefreshSabyCatalog(context.Context) (ChannelLinkResult, error)
}

type ActionExecution struct {
	Completed           bool
	ExternalOperationID string
	ExternalURL         string
	RetryAfter          time.Duration
}

type Service struct {
	store    Store
	parser   Parser
	executor Executor
}

func (service *Service) SabyPriceXLSX(ctx context.Context, orderID int64) ([]byte, string, error) {
	detail, err := service.store.OrderDetail(ctx, orderID)
	if err != nil {
		return nil, "", err
	}
	content, count, err := BuildSabyPriceXLSX(detail.Lines)
	if err != nil {
		return nil, "", err
	}
	name := fmt.Sprintf("saby-prices-%d-%d-items.xlsx", orderID, count)
	return content, name, nil
}

func NewService(store Store) *Service {
	return &Service{store: store, parser: NewPDFParser()}
}

func NewServiceWithParser(store Store, parser Parser) *Service {
	return &Service{store: store, parser: parser}
}

func NewServiceWithExecutor(store Store, executor Executor) *Service {
	return &Service{store: store, parser: NewPDFParser(), executor: executor}
}

func (service *Service) Dashboard(ctx context.Context) (Dashboard, error) {
	result, err := service.store.Dashboard(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	if service.executor != nil {
		result.Integrations = IntegrationStatus{
			WB: service.executor.Configured("wb"), Ozon: service.executor.Configured("ozon"),
			Saby: service.executor.Configured("saby"),
		}
	}
	health, err := service.store.ListIntegrationHealth(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	for index := range health {
		switch health[index].Channel {
		case "wb":
			health[index].Configured = result.Integrations.WB
		case "ozon":
			health[index].Configured = result.Integrations.Ozon
		case "saby":
			health[index].Configured = result.Integrations.Saby
		}
	}
	result.IntegrationHealth = health
	return result, nil
}

func (service *Service) CheckIntegration(ctx context.Context, actor Actor, channel string) (IntegrationHealth, error) {
	channel = strings.TrimSpace(channel)
	if !oneOf(channel, "wb", "ozon", "saby") {
		return IntegrationHealth{}, ErrInvalidInput
	}
	configured := service.executor != nil && service.executor.Configured(channel)
	var checkErr error
	if !configured {
		checkErr = errors.New("переменные окружения не настроены полностью")
	} else if prober, ok := service.executor.(IntegrationProber); !ok {
		checkErr = errors.New("проверка подключения не поддерживается")
	} else {
		checkErr = prober.Probe(ctx, channel)
	}
	item, err := service.store.RecordIntegrationCheck(ctx, channel, configured, checkErr)
	if err != nil {
		return IntegrationHealth{}, err
	}
	_ = actor // Kept in the service boundary for the integration audit extension.
	return item, nil
}

// SyncChannelCatalog подтягивает артикулы WB или Ozon и связывает их с
// номенклатурой СБИС по точному совпадению кода, артикула или штрихкода.
//
// Совпадение по названию сознательно не используется: «Фикус Бенджамина 12»
// и «Фикус Бенджамина 14» — разные растения с разной ценой, и ошибочная
// связь тихо припишет продажи чужой карточке.
func (service *Service) SyncChannelCatalog(ctx context.Context, actor Actor, channel string) (ChannelLinkResult, error) {
	channel = strings.TrimSpace(channel)
	if channel == "saby" {
		refresher, ok := service.executor.(SabyCatalogRefresher)
		if service.executor == nil || !ok {
			return ChannelLinkResult{}, errors.New("обновление справочника СБИС не поддерживается")
		}
		if !service.executor.Configured("saby") {
			return ChannelLinkResult{}, errors.New("ключи СБИС не настроены")
		}
		result, err := refresher.RefreshSabyCatalog(ctx)
		if err != nil {
			return ChannelLinkResult{}, err
		}
		_ = actor // actor remains at the service boundary for the audit extension.
		return result, nil
	}
	if !oneOf(channel, "wb", "ozon") {
		return ChannelLinkResult{}, ErrInvalidInput
	}
	source, ok := service.executor.(ChannelCatalogSource)
	if service.executor == nil || !ok {
		return ChannelLinkResult{}, errors.New("чтение справочника канала не поддерживается")
	}
	if !service.executor.Configured(channel) {
		return ChannelLinkResult{}, errors.New("ключи канала не настроены")
	}
	items, err := source.FetchCatalog(ctx, channel)
	if err != nil {
		return ChannelLinkResult{}, err
	}
	result, err := service.store.LinkChannelProducts(ctx, actor, channel, items)
	if err != nil {
		return ChannelLinkResult{}, err
	}
	// Названия карточек нужны разбору продаж: у Wildberries внешний код —
	// это числовой nmID, и без подписи человек не поймёт, какое растение
	// разбирает. Подпись вспомогательная, поэтому её неудача не отменяет
	// уже сделанное связывание артикулов.
	if remember, able := service.store.(SalesLinkStore); able {
		_ = remember.RememberChannelProducts(ctx, channel, items)
	}
	return result, nil
}

func (service *Service) UpdateSettings(ctx context.Context, actor Actor, input PricingSettings) (PricingSettings, error) {
	if input.TrolleyCostRUB == 0 && input.TrolleyCostCurrency > 0 {
		input.TrolleyCostRUB = input.TrolleyCostCurrency
	}
	if input.DefaultExchangeRate <= 0 || input.TrolleyCostCurrency < 0 || input.TrolleyCostRUB < 0 || input.TrolleyVolumeCM3 <= 0 ||
		input.TrolleyFillRatio <= 0 || input.TrolleyFillRatio > 1 || input.PackageRUB < 0 ||
		input.PriceChangeThreshold < 0 || input.PriceChangeThreshold >= 1 || input.RetailRoundStep <= 0 ||
		input.RecommendationDays < 7 || input.RecommendationDays > 365 || input.TargetCoverDays < 1 || input.TargetCoverDays > 365 ||
		!validRate(input.ReturnLossRate) || !validRate(input.MarketplaceCostRate) || !validRate(input.TaxRate) ||
		!validRate(input.ReserveRate) || !validRate(input.MarketplaceStrikeMarkup) ||
		input.DomesticRetailMultiplier <= 0 || input.InternationalCostMultiplier <= 0 || input.InternationalRetailMultiplier <= 0 ||
		input.RetailMarkupMultiplier <= 0 {
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
	input.TaxID = strings.TrimSpace(input.TaxID)
	input.KPP = strings.TrimSpace(input.KPP)
	input.DefaultCurrency = strings.ToUpper(strings.TrimSpace(input.DefaultCurrency))
	if input.Name == "" || !oneOf(input.Kind, KindInternational, KindDomestic) ||
		!oneOf(input.DefaultCurrency, "EUR", "USD", "RUB") || len(input.CountryCode) > 2 ||
		(input.TaxID != "" && (len(input.TaxID) != 10 && len(input.TaxID) != 12)) ||
		(input.KPP != "" && len(input.KPP) != 9) || (input.Kind == KindDomestic && len(input.TaxID) == 10 && input.KPP == "") {
		return Supplier{}, ErrInvalidInput
	}
	for _, char := range input.TaxID {
		if char < '0' || char > '9' {
			return Supplier{}, ErrInvalidInput
		}
	}
	for _, char := range input.KPP {
		if char < '0' || char > '9' {
			return Supplier{}, ErrInvalidInput
		}
	}
	return service.store.CreateSupplier(ctx, actor, input)
}

func (service *Service) DeleteSupplier(ctx context.Context, actor Actor, supplierID int64) error {
	if supplierID <= 0 {
		return ErrInvalidInput
	}
	return service.store.DeleteSupplier(ctx, actor, supplierID)
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
	if input.TrolleyCostRUB == 0 && input.TrolleyCostCurrency > 0 {
		input.TrolleyCostRUB = input.TrolleyCostCurrency
	}
	if orderID <= 0 || input.ExchangeRate <= 0 || input.TrolleyCostCurrency < 0 || input.TrolleyCostRUB < 0 ||
		input.DeliveryToMoscowRUB < 0 || input.DeliveryToRyazanRUB < 0 {
		return OrderDetail{}, ErrInvalidInput
	}
	return service.store.CalculateOrder(ctx, actor, orderID, input)
}

func (service *Service) UpdateOrderStatus(ctx context.Context, actor Actor, orderID int64, input OrderStatusUpdate) (OrderDetail, error) {
	input.Status = strings.TrimSpace(input.Status)
	input.Note = strings.TrimSpace(input.Note)
	if orderID <= 0 || !oneOf(input.Status, "received", "cancelled", "review") {
		return OrderDetail{}, ErrInvalidInput
	}
	return service.store.UpdateOrderStatus(ctx, actor, orderID, input)
}

func (service *Service) DeleteOrder(ctx context.Context, actor Actor, orderID int64) error {
	if orderID <= 0 {
		return ErrInvalidInput
	}
	return service.store.DeleteOrder(ctx, actor, orderID)
}

func (service *Service) UpdateOrderLine(ctx context.Context, actor Actor, lineID int64, input OrderLineUpdate) (OrderDetail, error) {
	if lineID <= 0 || (input.ExpectedUnitPrice == nil && input.PotDiameterCM == nil && input.HeightCM == nil && input.LoadUnit == nil && input.AcceptComparison == nil && input.ComparisonNote == nil) {
		return OrderDetail{}, ErrInvalidInput
	}
	if input.ExpectedUnitPrice != nil && *input.ExpectedUnitPrice < 0 || input.PotDiameterCM != nil && *input.PotDiameterCM <= 0 || input.HeightCM != nil && *input.HeightCM <= 0 {
		return OrderDetail{}, ErrInvalidInput
	}
	if input.LoadUnit != nil {
		value := strings.TrimSpace(*input.LoadUnit)
		input.LoadUnit = &value
	}
	if input.ComparisonNote != nil {
		value := strings.TrimSpace(*input.ComparisonNote)
		input.ComparisonNote = &value
	}
	if input.AcceptComparison != nil && *input.AcceptComparison && (input.ComparisonNote == nil || *input.ComparisonNote == "") {
		return OrderDetail{}, ErrInvalidInput
	}
	return service.store.UpdateOrderLine(ctx, actor, lineID, input)
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

func (service *Service) UpdateRequest(ctx context.Context, actor Actor, requestID int64, input RequestUpdate) (Request, error) {
	input.SabyID = strings.TrimSpace(input.SabyID)
	input.RequestedName = strings.TrimSpace(input.RequestedName)
	input.Status = strings.TrimSpace(input.Status)
	input.Notes = strings.TrimSpace(input.Notes)
	if requestID <= 0 || input.RequestedName == "" || input.Quantity <= 0 || !oneOf(input.Status, "open", "included", "fulfilled", "cancelled") {
		return Request{}, ErrInvalidInput
	}
	return service.store.UpdateRequest(ctx, actor, requestID, input)
}

func (service *Service) ListProducts(ctx context.Context, supplierID int64, query string) ([]ProductDirectoryItem, error) {
	query = strings.TrimSpace(query)
	if supplierID < 0 || len(query) > 200 {
		return nil, ErrInvalidInput
	}
	return service.store.ListProducts(ctx, supplierID, query)
}

func (service *Service) UpdateProduct(ctx context.Context, actor Actor, input ProductDirectoryUpdate) (ProductDirectoryItem, error) {
	input.SabyID = strings.TrimSpace(input.SabyID)
	input.SupplierArticle = strings.TrimSpace(input.SupplierArticle)
	input.AvailabilityStatus = strings.TrimSpace(input.AvailabilityStatus)
	input.CheckAfter = strings.TrimSpace(input.CheckAfter)
	input.HollandArticle = strings.TrimSpace(input.HollandArticle)
	input.WBVendorCode = strings.TrimSpace(input.WBVendorCode)
	input.OzonOfferID = strings.TrimSpace(input.OzonOfferID)
	if input.SabyID == "" || input.SupplierID <= 0 || input.MinimumOrderQty <= 0 || input.OrderMultiple <= 0 ||
		!oneOf(input.AvailabilityStatus, "available", "unknown", "check", "temporarily_unavailable", "discontinued") ||
		input.WBNmID != nil && *input.WBNmID <= 0 {
		return ProductDirectoryItem{}, ErrInvalidInput
	}
	if input.CheckAfter != "" {
		if _, err := time.Parse("2006-01-02", input.CheckAfter); err != nil {
			return ProductDirectoryItem{}, ErrInvalidInput
		}
	}
	return service.store.UpdateProduct(ctx, actor, input)
}

func (service *Service) UpdateAvailability(ctx context.Context, actor Actor, input AvailabilityUpdate) (AvailabilityItem, error) {
	input.SabyID = strings.TrimSpace(input.SabyID)
	input.Status = strings.TrimSpace(input.Status)
	input.CheckAfter = strings.TrimSpace(input.CheckAfter)
	if input.SupplierID <= 0 || input.SabyID == "" ||
		!oneOf(input.Status, "available", "unknown", "check", "temporarily_unavailable", "discontinued") {
		return AvailabilityItem{}, ErrInvalidInput
	}
	if input.CheckAfter != "" {
		if _, err := time.Parse("2006-01-02", input.CheckAfter); err != nil {
			return AvailabilityItem{}, ErrInvalidInput
		}
	}
	return service.store.UpdateAvailability(ctx, actor, input)
}

// SetExclusion снимает товар с закупки. Причина обязательна: список «не
// закупаем» живёт годами, и через полгода вопрос «почему этого нет в
// рекомендациях» задаст тот же человек, который его туда положил.
func (service *Service) SetExclusion(ctx context.Context, actor Actor, input ExclusionUpdate) error {
	input.SabyID = strings.TrimSpace(input.SabyID)
	input.Reason = strings.TrimSpace(input.Reason)
	if len([]rune(input.Reason)) > 300 {
		input.Reason = string([]rune(input.Reason)[:300])
	}
	if input.SabyID == "" || (input.Excluded && input.Reason == "") {
		return ErrInvalidInput
	}
	return service.store.SetExclusion(ctx, actor, input)
}

func (service *Service) PrepareBatch(ctx context.Context, actor Actor, orderID int64, kind string, channels []string) (ActionBatch, error) {
	kind = strings.TrimSpace(kind)
	if orderID <= 0 || !oneOf(kind, "receipt", "prices") {
		return ActionBatch{}, ErrInvalidInput
	}
	selected, seen := make([]string, 0, len(channels)), map[string]bool{}
	for _, channel := range channels {
		channel = strings.TrimSpace(channel)
		if !oneOf(channel, "site", "wb", "ozon", "saby_price") || seen[channel] {
			continue
		}
		seen[channel], selected = true, append(selected, channel)
	}
	if kind == "prices" && len(selected) == 0 {
		return ActionBatch{}, ErrInvalidInput
	}
	// Старая цена площадки нужна не только для показа: оператор должен
	// видеть реальное изменение до подтверждения. Обновляем карточки прямо
	// перед снимком batch, чтобы не подставлять вчерашнее значение.
	if kind == "prices" {
		source, canRead := service.executor.(ChannelCatalogSource)
		remember, canRemember := service.store.(interface {
			RememberChannelProducts(context.Context, string, []ChannelProduct) error
		})
		for _, channel := range selected {
			if channel != "wb" && channel != "ozon" {
				continue
			}
			if service.executor == nil || !service.executor.Configured(channel) || !canRead || !canRemember {
				return ActionBatch{}, &UserFacingError{Message: channelDisplayName(channel) + ": подключение не настроено, текущую цену получить нельзя"}
			}
			items, err := source.FetchCatalog(ctx, channel)
			if err != nil {
				return ActionBatch{}, &UserFacingError{Message: fmt.Sprintf("%s: не удалось получить карточки и текущие цены: %v", channelDisplayName(channel), err)}
			}
			if err := remember.RememberChannelProducts(ctx, channel, items); err != nil {
				return ActionBatch{}, fmt.Errorf("%s: сохранить текущие цены: %w", channelDisplayName(channel), err)
			}
		}
	}
	return service.store.PrepareBatch(ctx, actor, orderID, kind, selected)
}

func channelDisplayName(channel string) string {
	if channel == "wb" {
		return "Wildberries"
	}
	if channel == "ozon" {
		return "Ozon"
	}
	return channel
}

func (service *Service) ApproveBatch(ctx context.Context, actor Actor, batchID int64) (ActionBatch, error) {
	if batchID <= 0 {
		return ActionBatch{}, ErrInvalidInput
	}
	configured := map[string]bool{}
	if service.executor != nil {
		for _, channel := range []string{"wb", "ozon", "saby_price", "saby_receipt"} {
			configured[channel] = service.executor.Configured(channel)
		}
	}
	return service.store.ApproveBatch(ctx, actor, batchID, configured)
}

func (service *Service) RetryBatch(ctx context.Context, actor Actor, batchID int64) (ActionBatch, error) {
	if batchID <= 0 {
		return ActionBatch{}, ErrInvalidInput
	}
	configured := map[string]bool{}
	if service.executor != nil {
		for _, channel := range []string{"wb", "ozon", "saby_price", "saby_receipt"} {
			configured[channel] = service.executor.Configured(channel)
		}
	}
	return service.store.RetryBatch(ctx, actor, batchID, configured)
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
