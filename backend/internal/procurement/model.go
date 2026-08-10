package procurement

import (
	"errors"
	"time"
)

var (
	ErrInvalidInput        = errors.New("invalid procurement input")
	ErrNotFound            = errors.New("procurement entity not found")
	ErrDuplicate           = errors.New("procurement document already imported")
	ErrUnsupportedDocument = errors.New("unsupported procurement document")
)

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
	Summary   Summary           `json:"summary"`
	Suppliers []Supplier        `json:"suppliers"`
	Orders    []OrderSummary    `json:"orders"`
	Documents []DocumentSummary `json:"documents"`
	Review    []AliasReview     `json:"review"`
}

type Supplier struct {
	ID              int64     `json:"id"`
	Name            string    `json:"name"`
	Kind            string    `json:"kind"`
	CountryCode     string    `json:"countryCode"`
	DefaultCurrency string    `json:"defaultCurrency"`
	Active          bool      `json:"active"`
	CreatedAt       time.Time `json:"createdAt"`
}

type SupplierCreate struct {
	Name            string `json:"name"`
	Kind            string `json:"kind"`
	CountryCode     string `json:"countryCode"`
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
