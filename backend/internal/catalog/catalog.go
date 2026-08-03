package catalog

import (
	"context"
	"errors"
)

var ErrNotFound = errors.New("product not found")

type Product struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Latin    string  `json:"latin"`
	Category string  `json:"category"`
	Price    float64 `json:"price"`
	Image    string  `json:"image"`
	Light    string  `json:"light"`
	Size     string  `json:"size"`
	Stock    int     `json:"stock"`
}

type ProductDetail struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	Latin            string    `json:"latin"`
	ShortDescription string    `json:"shortDescription"`
	Description      string    `json:"description"`
	CareInstructions string    `json:"careInstructions"`
	Images           []string  `json:"images"`
	Variants         []Variant `json:"variants"`
	Recommendations  []Product `json:"recommendations"`
}

type Variant struct {
	ID              int64   `json:"id"`
	SKU             string  `json:"sku"`
	Label           string  `json:"label"`
	Price           float64 `json:"price"`
	Stock           int     `json:"stock"`
	HeightCM        *int    `json:"heightCm"`
	PotDiameterCM   *int    `json:"potDiameterCm"`
	WholesaleMinQty int     `json:"wholesaleMinQty"`
}

type Repository interface {
	ListAvailable(context.Context) ([]Product, error)
	DetailBySlug(context.Context, string) (ProductDetail, error)
}
