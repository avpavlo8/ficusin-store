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

type Repository interface {
	ListForCustomer(context.Context, int64, int) ([]Summary, error)
}
