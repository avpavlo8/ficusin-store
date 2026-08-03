package catalog

import (
	"context"
	"errors"
)

var ErrNotFound = errors.New("product not found")

type Category struct {
	ID        int64  `json:"id"`
	ParentID  *int64 `json:"parentId"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	SortOrder int    `json:"sortOrder"`
}

type Product struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Latin    string  `json:"latin"`
	Category string  `json:"category"`
	Price    float64 `json:"price"`
	Image    string  `json:"image"`
	Light    string  `json:"light"`
	Size     string  `json:"size"`
	Stock          int     `json:"stock"`
	CatalogSection string  `json:"catalogSection"`
	PlantKind      string  `json:"plantKind,omitempty"`
	LightLevel     string  `json:"lightLevel,omitempty"`
	Watering       string  `json:"watering,omitempty"`
	HeightClass    string  `json:"heightClass,omitempty"`
	CareLevel      string  `json:"careLevel,omitempty"`
	Placement      string  `json:"placement,omitempty"`
	PetSafety      string  `json:"petSafety,omitempty"`
	GrowthHabit    string  `json:"growthHabit,omitempty"`
	CategoryID     *int64  `json:"categoryId,omitempty"`
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
	Recommendations []Product `json:"recommendations"`
	CatalogSection string    `json:"catalogSection"`
	PlantKind      string    `json:"plantKind,omitempty"`
	LightLevel     string    `json:"lightLevel,omitempty"`
	Watering       string    `json:"watering,omitempty"`
	HeightClass    string    `json:"heightClass,omitempty"`
	CareLevel      string    `json:"careLevel,omitempty"`
	Placement      string    `json:"placement,omitempty"`
	PetSafety      string    `json:"petSafety,omitempty"`
	GrowthHabit    string    `json:"growthHabit,omitempty"`
	CategoryID     *int64    `json:"categoryId,omitempty"`
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
	ListCategories(context.Context) ([]Category, error)
	DetailBySlug(context.Context, string) (ProductDetail, error)
}
