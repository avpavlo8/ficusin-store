package order

import (
	"context"
	"time"
)

type Summary struct {
	OrderNumber    string    `json:"orderNumber"`
	DeliveryMethod string    `json:"deliveryMethod"`
	Total          float64   `json:"total"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"createdAt"`
	ItemsCount     int       `json:"itemsCount"`
}

// Detail is a single order opened from the account page: the summary plus
// everything the customer needs to check what they ordered and where it is
// going.
type Detail struct {
	OrderNumber    string    `json:"orderNumber"`
	DeliveryMethod string    `json:"deliveryMethod"`
	Address        string    `json:"address"`
	Comment        string    `json:"comment"`
	CancellationReason string `json:"cancellationReason"`
	CustomerName   string    `json:"customerName"`
	Phone          string    `json:"phone"`
	Email          string    `json:"email"`
	Status         string    `json:"status"`
	PaymentStatus  string    `json:"paymentStatus"`
	PaymentMethod  string    `json:"paymentMethod"`
	TrackNumber string `json:"trackNumber"`
	HasPreorder bool   `json:"hasPreorder"`
	DeliveryFee    float64   `json:"deliveryFee"`
	DeliveryFeePending bool `json:"deliveryFeePending"`
	RepackRequested    bool `json:"repackRequested"`
	Subtotal       float64   `json:"subtotal"`
	Total          float64   `json:"total"`
	PaidAmount     float64   `json:"paidAmount"`
	RefundedAmount float64   `json:"refundedAmount"`
	AmountDue      float64   `json:"amountDue"`
	PaymentReady   bool      `json:"paymentReady"`
	CreatedAt      time.Time `json:"createdAt"`
	Items          []Item    `json:"items"`
	ShipmentOffers []ShipmentOffer `json:"shipmentOffers"`
}

type Item struct {
	ProductName string  `json:"productName"`
	UnitPrice   float64 `json:"unitPrice"`
	OriginalUnitPrice float64 `json:"originalUnitPrice,omitempty"`
	Quantity    int     `json:"quantity"`
}

type ShipmentOffer struct {
	ID int64 `json:"id"`
	PaymentToken string `json:"paymentToken,omitempty"`
	Status string `json:"status"`
	DeliveryFee float64 `json:"deliveryFee"`
	Subtotal float64 `json:"subtotal"`
	Total float64 `json:"total"`
	NotifiedAt *time.Time `json:"notifiedAt,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	Items []Item `json:"items"`
	Boxes int `json:"boxes"`
}

type Repository interface {
	ListForCustomer(context.Context, int64, int) ([]Summary, error)
	DetailForCustomer(ctx context.Context, customerID int64, orderNumber string) (*Detail, error)
}
