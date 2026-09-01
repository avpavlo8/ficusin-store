package procurement

import (
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrInvalidInput        = errors.New("invalid procurement input")
	ErrNotFound            = errors.New("procurement entity not found")
	ErrDuplicate           = errors.New("procurement document already imported")
	ErrOrderNotCancelled   = errors.New("procurement order is not cancelled")
	ErrSupplierInUse       = errors.New("procurement supplier is in use")
	ErrUnsupportedDocument = errors.New("unsupported procurement document")
)

// UserFacingError is a checked integration/preflight problem that an
// operator can fix. Unlike an internal database error, its text is safe and
// useful to return to the admin interface verbatim.
type UserFacingError struct{ Message string }

func (err *UserFacingError) Error() string { return err.Message }

const (
	KindInternational = "international"
	KindDomestic      = "domestic"

	SourceRecommendation = "recommendation"
	SourceManual         = "manual"
	SourceInvoice        = "invoice"
	SourcePaymentInvoice = "payment_invoice"
)

type Actor struct {
	CustomerID int64
	Role       string
}

type Summary struct {
	OpenOrders         int `json:"openOrders"`
	UnresolvedAliases  int `json:"unresolvedAliases"`
	AvailabilityChecks int `json:"availabilityChecks"`
	OpenRequests       int `json:"openRequests"`
}

type Dashboard struct {
	Summary           Summary             `json:"summary"`
	Integrations      IntegrationStatus   `json:"integrations"`
	Settings          PricingSettings     `json:"settings"`
	Suppliers         []Supplier          `json:"suppliers"`
	Orders            []OrderSummary      `json:"orders"`
	Documents         []DocumentSummary   `json:"documents"`
	Review            []AliasReview       `json:"review"`
	Requests          []Request           `json:"requests"`
	Availability      []AvailabilityItem  `json:"availability"`
	Recommendations   []Recommendation    `json:"recommendations"`
	SalesSync         []SalesSyncStatus   `json:"salesSync"`
	IntegrationHealth []IntegrationHealth `json:"integrationHealth"`
}

type PricingSettings struct {
	Version                       int     `json:"version"`
	DefaultExchangeRate           float64 `json:"defaultExchangeRate"`
	TrolleyCostCurrency           float64 `json:"trolleyCostCurrency"`
	TrolleyCostRUB                float64 `json:"trolleyCostRub"`
	TrolleyVolumeCM3              float64 `json:"trolleyVolumeCm3"`
	TrolleyFillRatio              float64 `json:"trolleyFillRatio"`
	ReturnLossRate                float64 `json:"returnLossRate"`
	MarketplaceCostRate           float64 `json:"marketplaceCostRate"`
	TaxRate                       float64 `json:"taxRate"`
	ReserveRate                   float64 `json:"reserveRate"`
	PackageRUB                    float64 `json:"packageRub"`
	PriceChangeThreshold          float64 `json:"priceChangeThreshold"`
	DomesticRetailMultiplier      float64 `json:"domesticRetailMultiplier"`
	InternationalCostMultiplier   float64 `json:"internationalCostMultiplier"`
	InternationalRetailMultiplier float64 `json:"internationalRetailMultiplier"`
	MarketplaceStrikeMarkup       float64 `json:"marketplaceStrikeMarkup"`
	RetailRoundStep               int     `json:"retailRoundStep"`
	AvoidRoundHundreds            bool    `json:"avoidRoundHundreds"`
	RecommendationDays            int     `json:"recommendationDays"`
	TargetCoverDays               int     `json:"targetCoverDays"`
	RetailMarkupMultiplier        float64 `json:"retailMarkupMultiplier"`
	RoundPrices                   bool    `json:"roundPrices"`
	// MarketplaceLogisticsPerCM — сколько рублей за сантиметр высоты
	// растения площадка берёт за доставку. В исходной книге это был
	// столбец «Логистика» = высота × 10.
	MarketplaceLogisticsPerCM float64 `json:"marketplaceLogisticsPerCm"`
}

type Supplier struct {
	ID              int64     `json:"id"`
	Name            string    `json:"name"`
	Kind            string    `json:"kind"`
	CountryCode     string    `json:"countryCode"`
	TaxID           string    `json:"taxId"`
	KPP             string    `json:"kpp"`
	DefaultCurrency string    `json:"defaultCurrency"`
	Active          bool      `json:"active"`
	CreatedAt       time.Time `json:"createdAt"`
}

type SupplierCreate struct {
	Name            string `json:"name"`
	Kind            string `json:"kind"`
	CountryCode     string `json:"countryCode"`
	TaxID           string `json:"taxId"`
	KPP             string `json:"kpp"`
	DefaultCurrency string `json:"defaultCurrency"`
}

type OrderSummary struct {
	ID             int64      `json:"id"`
	SupplierID     int64      `json:"supplierId"`
	SupplierName   string     `json:"supplierName"`
	OrderNumber    string     `json:"orderNumber"`
	DocumentNumber string     `json:"documentNumber"`
	DocumentDate   *time.Time `json:"documentDate,omitempty"`
	SourceKind     string     `json:"sourceKind"`
	Currency       string     `json:"currency"`
	Status         string     `json:"status"`
	Lines          int        `json:"lines"`
	Units          int        `json:"units"`
	Total          float64    `json:"total"`
	Unmatched      int        `json:"unmatched"`
	CreatedAt      time.Time  `json:"createdAt"`
}

type OrderCreate struct {
	SupplierID  int64  `json:"supplierId"`
	OrderNumber string `json:"orderNumber"`
	SourceKind  string `json:"sourceKind"`
	Currency    string `json:"currency"`
	Notes       string `json:"notes"`
}

type PlanCreate struct {
	SupplierID  int64      `json:"supplierId"`
	OrderNumber string     `json:"orderNumber"`
	Items       []PlanItem `json:"items"`
}

type PlanItem struct {
	SabyID            string  `json:"sabyId"`
	Quantity          int     `json:"quantity"`
	ExpectedUnitPrice float64 `json:"expectedUnitPrice"`
}

type OrderCosts struct {
	ExchangeRate        float64 `json:"exchangeRate"`
	TrolleyCostCurrency float64 `json:"trolleyCostCurrency"`
	TrolleyCostRUB      float64 `json:"trolleyCostRub"`
	DeliveryToMoscowRUB float64 `json:"deliveryToMoscowRub"`
	DeliveryToRyazanRUB float64 `json:"deliveryToRyazanRub"`
}

type OrderDetail struct {
	Order      OrderSummary    `json:"order"`
	Costs      OrderCosts      `json:"costs"`
	Validation OrderValidation `json:"validation"`
	Lines      []OrderLine     `json:"lines"`
	Batches    []ActionBatch   `json:"batches"`
}

type OrderValidation struct {
	CanCalculate        bool     `json:"canCalculate"`
	CanPrepareActions   bool     `json:"canPrepareActions"`
	Blockers            []string `json:"blockers"`
	ArithmeticMismatch  int      `json:"arithmeticMismatch"`
	ComparisonMismatch  int      `json:"comparisonMismatch"`
	MissingDimensions   int      `json:"missingDimensions"`
	MissingLoadUnits    int      `json:"missingLoadUnits"`
	InvalidLines        int      `json:"invalidLines"`
	Unmatched           int      `json:"unmatched"`
	TrolleyCount        int      `json:"trolleyCount"`
	ExpectedTrolleyRUB  float64  `json:"expectedTrolleyRub"`
	AllocatedTrolleyRUB float64  `json:"allocatedTrolleyRub"`
	ExpectedRyazanRUB   float64  `json:"expectedRyazanRub"`
	AllocatedRyazanRUB  float64  `json:"allocatedRyazanRub"`
}

type OrderLine struct {
	ID                           int64    `json:"id"`
	SabyID                       string   `json:"sabyId"`
	SabyCode                     string   `json:"sabyCode"`
	SabyName                     string   `json:"sabyName"`
	RawName                      string   `json:"rawName"`
	SupplierArticle              string   `json:"supplierArticle"`
	Quantity                     int      `json:"quantity"`
	OrderedQuantity              int      `json:"orderedQuantity"`
	InvoicedQuantity             *int     `json:"invoicedQuantity,omitempty"`
	UnitPrice                    float64  `json:"unitPrice"`
	ExpectedUnitPrice            *float64 `json:"expectedUnitPrice,omitempty"`
	LoadUnit                     string   `json:"loadUnit"`
	PotDiameterCM                *float64 `json:"potDiameterCm,omitempty"`
	HeightCM                     *float64 `json:"heightCm,omitempty"`
	MatchStatus                  string   `json:"matchStatus"`
	PurchaseUnitRUB              *float64 `json:"purchaseUnitRub,omitempty"`
	TrolleyDeliveryUnitRUB       *float64 `json:"trolleyDeliveryUnitRub,omitempty"`
	RyazanDeliveryUnitRUB        *float64 `json:"ryazanDeliveryUnitRub,omitempty"`
	UnitCostRUB                  *float64 `json:"unitCostRub,omitempty"`
	CurrentRetailRUB             float64  `json:"currentRetailRub"`
	ProposedRetailRUB            *int64   `json:"proposedRetailRub,omitempty"`
	ProposedMarketplaceRUB       *int64   `json:"proposedMarketplaceRub,omitempty"`
	ProposedMarketplaceStrikeRUB *int64   `json:"proposedMarketplaceStrikeRub,omitempty"`
	PriceChangeNeeded            bool     `json:"priceChangeNeeded"`
	CustomerRequest              bool     `json:"customerRequest"`
	ComparisonMismatch           bool     `json:"comparisonMismatch"`
	ComparisonAccepted           bool     `json:"comparisonAccepted"`
	ComparisonNote               string   `json:"comparisonNote"`
}

type CalculationInput struct {
	ExchangeRate        float64 `json:"exchangeRate"`
	TrolleyCostCurrency float64 `json:"trolleyCostCurrency"`
	TrolleyCostRUB      float64 `json:"trolleyCostRub"`
	DeliveryToMoscowRUB float64 `json:"deliveryToMoscowRub"`
	DeliveryToRyazanRUB float64 `json:"deliveryToRyazanRub"`
}

type Request struct {
	ID            int64     `json:"id"`
	Kind          string    `json:"kind"`
	SabyID        string    `json:"sabyId"`
	RequestedName string    `json:"requestedName"`
	Quantity      int       `json:"quantity"`
	Status        string    `json:"status"`
	Notes         string    `json:"notes"`
	CreatedAt     time.Time `json:"createdAt"`
}

type RequestCreate struct {
	Kind          string `json:"kind"`
	SabyID        string `json:"sabyId"`
	RequestedName string `json:"requestedName"`
	Quantity      int    `json:"quantity"`
	Notes         string `json:"notes"`
}

type RequestUpdate struct {
	SabyID        string `json:"sabyId"`
	RequestedName string `json:"requestedName"`
	Quantity      int    `json:"quantity"`
	Status        string `json:"status"`
	Notes         string `json:"notes"`
}

type OrderStatusUpdate struct {
	Status string `json:"status"`
	Note   string `json:"note"`
}

type OrderLineUpdate struct {
	ExpectedUnitPrice *float64 `json:"expectedUnitPrice"`
	PotDiameterCM     *float64 `json:"potDiameterCm"`
	HeightCM          *float64 `json:"heightCm"`
	LoadUnit          *string  `json:"loadUnit"`
	AcceptComparison  *bool    `json:"acceptComparison"`
	ComparisonNote    *string  `json:"comparisonNote"`
}

// AvailabilityUpdate — наличие у поставщика. Ключ — пара поставщик+товар,
// а не написание из инвойса: пометить нужно уметь и товар, которого ни в
// одном разобранном PDF ещё не было.
type AvailabilityUpdate struct {
	SupplierID int64  `json:"supplierId"`
	SabyID     string `json:"sabyId"`
	Status     string `json:"status"`
	CheckAfter string `json:"checkAfter"`
}

// AvailabilityItem — строка раздела «Наличие у поставщика».
type AvailabilityItem struct {
	SupplierID       int64      `json:"supplierId"`
	SupplierName     string     `json:"supplierName"`
	SabyID           string     `json:"sabyId"`
	Name             string     `json:"name"`
	SupplierArticle  string     `json:"supplierArticle"`
	Status           string     `json:"availabilityStatus"`
	CheckAfter       string     `json:"checkAfter"`
	UnavailableSince string     `json:"unavailableSince"`
	Balance          int        `json:"balance"`
	LastSeenAt       *time.Time `json:"lastSeenAt,omitempty"`
}

// ExclusionUpdate — «не закупаем». Решение магазина, а не поставщика,
// поэтому крепится к товару целиком.
type ExclusionUpdate struct {
	SabyID   string `json:"sabyId"`
	Excluded bool   `json:"excluded"`
	Reason   string `json:"reason"`
}

type Recommendation struct {
	AliasID          int64      `json:"aliasId"`
	SupplierID       int64      `json:"supplierId"`
	SabyID           string     `json:"sabyId"`
	Name             string     `json:"name"`
	SupplierArticle  string     `json:"supplierArticle"`
	Availability     string     `json:"availability"`
	Balance          int        `json:"balance"`
	Incoming         int        `json:"incoming"`
	SiteSales        int        `json:"siteSales"`
	SabySales        int        `json:"sabySales"`
	WBSales          int        `json:"wbSales"`
	OzonSales        int        `json:"ozonSales"`
	TotalSales       int        `json:"totalSales"`
	CustomerRequests int        `json:"customerRequests"`
	StaffRequests    int        `json:"staffRequests"`
	OpenRequests     int        `json:"openRequests"`
	MinimumOrderQty  int        `json:"minimumOrderQty"`
	OrderMultiple    int        `json:"orderMultiple"`
	SuggestedQty     int        `json:"suggestedQty"`
	DailySales       float64    `json:"dailySales"`
	DaysOfCover      *float64   `json:"daysOfCover,omitempty"`
	LastOrderedAt    *time.Time `json:"lastOrderedAt,omitempty"`
	Status           string     `json:"status"`
	Reason           string     `json:"reason"`
}

type SalesRecord struct {
	Date       time.Time
	ExternalID string
	SabyID     string
	Units      int
	GrossRUB   float64
}

type SalesSyncStatus struct {
	Channel       string     `json:"channel"`
	Status        string     `json:"status"`
	LastAttemptAt *time.Time `json:"lastAttemptAt,omitempty"`
	LastSuccessAt *time.Time `json:"lastSuccessAt,omitempty"`
	LastError     string     `json:"lastError"`
	RowsSynced    int        `json:"rowsSynced"`
	RowsLinked    int        `json:"rowsLinked"`
	PeriodFrom    string     `json:"periodFrom"`
	PeriodTo      string     `json:"periodTo"`
	LatestSale    string     `json:"latestSale"`
}

type ProductDirectoryItem struct {
	VariantID          int64    `json:"variantId"`
	SabyID             string   `json:"sabyId"`
	SabyCode           string   `json:"sabyCode"`
	SabyArticle        string   `json:"sabyArticle"`
	Name               string   `json:"name"`
	Balance            int      `json:"balance"`
	CurrentPriceRUB    float64  `json:"currentPriceRub"`
	SupplierID         int64    `json:"supplierId"`
	SupplierName       string   `json:"supplierName"`
	SupplierArticle    string   `json:"supplierArticle"`
	AvailabilityStatus string   `json:"availabilityStatus"`
	CheckAfter         string   `json:"checkAfter"`
	HollandArticle     string   `json:"hollandArticle"`
	WBNmID             *int64   `json:"wbNmId,omitempty"`
	WBVendorCode       string   `json:"wbVendorCode"`
	OzonOfferID        string   `json:"ozonOfferId"`
	WBArticles         []string `json:"wbArticles"`
	WBLegacyArticles   []string `json:"wbLegacyArticles"`
	OzonArticles       []string `json:"ozonArticles"`
	OzonLegacyArticles []string `json:"ozonLegacyArticles"`
	MinimumOrderQty    int      `json:"minimumOrderQty"`
	OrderMultiple      int      `json:"orderMultiple"`
	Aliases            []string `json:"aliases"`
	AliasIDs           []int64  `json:"aliasIds"`
}

type ProductDirectoryUpdate struct {
	VariantID          int64  `json:"variantId"`
	SabyID             string `json:"sabyId"`
	SupplierID         int64  `json:"supplierId"`
	SupplierArticle    string `json:"supplierArticle"`
	AvailabilityStatus string `json:"availabilityStatus"`
	CheckAfter         string `json:"checkAfter"`
	HollandArticle     string `json:"hollandArticle"`
	WBNmID             *int64 `json:"wbNmId"`
	WBVendorCode       string `json:"wbVendorCode"`
	OzonOfferID        string `json:"ozonOfferId"`
	MinimumOrderQty    int    `json:"minimumOrderQty"`
	OrderMultiple      int    `json:"orderMultiple"`
}

type ActionBatch struct {
	ID        int64        `json:"id"`
	Kind      string       `json:"kind"`
	Status    string       `json:"status"`
	CreatedAt time.Time    `json:"createdAt"`
	Items     []ActionItem `json:"items"`
}

type ActionItem struct {
	ID                  int64               `json:"id"`
	LineID              int64               `json:"lineId"`
	ProductName         string              `json:"productName"`
	ProductCode         string              `json:"productCode"`
	Channel             string              `json:"channel"`
	ExternalArticle     string              `json:"externalArticle"`
	DisplayArticle      string              `json:"displayArticle"`
	OldValue            *float64            `json:"oldValue,omitempty"`
	NewValue            float64             `json:"newValue"`
	CompareAtValue      *float64            `json:"compareAtValue,omitempty"`
	Quantity            *int                `json:"quantity,omitempty"`
	Status              string              `json:"status"`
	ErrorMessage        string              `json:"errorMessage"`
	ExternalOperationID string              `json:"externalOperationId,omitempty"`
	ExternalURL         string              `json:"externalUrl,omitempty"`
	PreviewLines        []ActionPreviewLine `json:"previewLines,omitempty"`
	Payload             json.RawMessage     `json:"-"`
	Attempts            int                 `json:"-"`
}

// ActionPreviewLine exposes every line of one aggregate Saby document.
type ActionPreviewLine struct {
	SabyID     string  `json:"sabyId"`
	Code       string  `json:"code"`
	Name       string  `json:"name"`
	Quantity   int     `json:"quantity,omitempty"`
	OldBalance int     `json:"oldBalance,omitempty"`
	NewBalance int     `json:"newBalance,omitempty"`
	OldPrice   float64 `json:"oldPrice,omitempty"`
	NewPrice   float64 `json:"newPrice,omitempty"`
}

// ChannelProduct — карточка маркетплейса, какой её отдаёт площадка.
type ChannelProduct struct {
	ExternalID       string
	Article          string
	Name             string
	Barcodes         []string
	CurrentPrice     *float64
	CurrentBasePrice *float64
}

// ChannelLinkResult — итог подтягивания справочника канала.
//
// Одних счётчиков мало: «связано 0» ничего не говорит о том, почему.
// Поэтому рядом лежит то, по чему шло сравнение — сколько ключей нашлось
// с каждой стороны и как они выглядят. Примеры берутся из артикулов и
// штрихкодов, то есть из того, что и так печатается на ценнике.
type ChannelLinkResult struct {
	Channel        string   `json:"channel"`
	Fetched        int      `json:"fetched"`
	Linked         int      `json:"linked"`
	Unmatched      int      `json:"unmatched"`
	ChannelKeys    int      `json:"channelKeys"`
	CatalogKeys    int      `json:"catalogKeys"`
	ChannelSamples []string `json:"channelSamples"`
	CatalogSamples []string `json:"catalogSamples"`
}

type IntegrationStatus struct {
	WB   bool `json:"wb"`
	Ozon bool `json:"ozon"`
	Saby bool `json:"saby"`
}

type IntegrationHealth struct {
	Channel       string     `json:"channel"`
	Configured    bool       `json:"configured"`
	LastCheckedAt *time.Time `json:"lastCheckedAt,omitempty"`
	LastSuccessAt *time.Time `json:"lastSuccessAt,omitempty"`
	LastError     string     `json:"lastError"`
}

type AliasReview struct {
	ID                 int64      `json:"id"`
	SupplierID         int64      `json:"supplierId"`
	SupplierName       string     `json:"supplierName"`
	RawName            string     `json:"rawName"`
	SupplierArticle    string     `json:"supplierArticle"`
	PotDiameterCM      *float64   `json:"potDiameterCm,omitempty"`
	HeightCM           *float64   `json:"heightCm,omitempty"`
	SuggestedSabyID    string     `json:"suggestedSabyId"`
	SuggestedSabyName  string     `json:"suggestedSabyName"`
	MatchStatus        string     `json:"matchStatus"`
	Confidence         float64    `json:"confidence"`
	AvailabilityStatus string     `json:"availabilityStatus"`
	LastSeenAt         *time.Time `json:"lastSeenAt,omitempty"`
}

type NomenclatureCandidate struct {
	VariantID int64   `json:"variantId"`
	SabyID   string  `json:"sabyId"`
	Code     string  `json:"code"`
	Article  string  `json:"article"`
	Name     string  `json:"name"`
	Balance  int     `json:"balance"`
	Price    float64 `json:"price"`
}

type AliasResolution struct {
	MatchStatus string `json:"matchStatus"`
	SabyID      string `json:"sabyId"`
}

type DocumentSummary struct {
	ID               int64      `json:"id"`
	SupplierID       int64      `json:"supplierId"`
	SupplierName     string     `json:"supplierName"`
	OrderID          int64      `json:"orderId"`
	FileName         string     `json:"fileName"`
	ParserKind       string     `json:"parserKind"`
	ParseStatus      string     `json:"parseStatus"`
	ArithmeticStatus string     `json:"arithmeticStatus"`
	DocumentNumber   string     `json:"documentNumber"`
	DocumentDate     *time.Time `json:"documentDate,omitempty"`
	Currency         string     `json:"currency"`
	Lines            int        `json:"lines"`
	Units            int        `json:"units"`
	ProductSubtotal  float64    `json:"productSubtotal"`
	PackageTotal     float64    `json:"packageTotal"`
	DocumentTotal    float64    `json:"documentTotal"`
	CalculatedTotal  float64    `json:"calculatedTotal"`
	ParseError       string     `json:"parseError"`
	CreatedAt        time.Time  `json:"createdAt"`
}

type DocumentUpload struct {
	SupplierID  int64
	OrderID     int64
	FileName    string
	ContentType string
	Content     []byte
}

type ImportResult struct {
	Document  DocumentSummary `json:"document"`
	Order     OrderSummary    `json:"order"`
	Duplicate bool            `json:"duplicate"`
}

type ParsedDocument struct {
	ParserKind      string
	DocumentNumber  string
	DocumentDate    *time.Time
	Currency        string
	ProductSubtotal float64
	PackageTotal    float64
	DocumentTotal   float64
	CalculatedTotal float64
	ArithmeticOK    bool
	ExtractedText   string
	Lines           []ParsedLine
}

type ParsedLine struct {
	SourcePage      int
	SourceLine      int
	RawName         string
	SupplierArticle string
	Quantity        int
	UnitPrice       float64
	LineTotal       float64
	LoadUnit        string
	PotDiameterCM   *float64
	HeightCM        *float64
}
