package procurement

import (
	"errors"
	"time"
)

var (
	ErrInvalidInput = errors.New("invalid procurement input")
	ErrNotFound     = errors.New("procurement entity not found")
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
	OpenOrders        int `json:"openOrders"`
	UnresolvedAliases int `json:"unresolvedAliases"`
	AvailabilityChecks int `json:"availabilityChecks"`
	OpenRequests      int `json:"openRequests"`
}

type Dashboard struct {
	Summary   Summary        `json:"summary"`
	Suppliers []Supplier     `json:"suppliers"`
	Orders    []OrderSummary `json:"orders"`
	Review    []AliasReview  `json:"review"`
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
	SupplierID   int64  `json:"supplierId"`
	OrderNumber  string `json:"orderNumber"`
	SourceKind   string `json:"sourceKind"`
	Currency     string `json:"currency"`
	Notes        string `json:"notes"`
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
