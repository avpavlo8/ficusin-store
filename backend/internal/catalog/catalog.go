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
	// Collections are the hand-made sets this product belongs to, by slug.
	// The storefront filters on them without another request.
	Collections []string `json:"collections"`
	Rating float64 `json:"rating"`
	ReviewsCount int `json:"reviewsCount"`
}

// Collection is one hand-made set as the storefront tab shows it.
type Collection struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
	Note  string `json:"note"`
	Count int    `json:"count"`
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
	Passport       PlantPassport `json:"passport"`
	ImportantWarnings []string `json:"importantWarnings"`
	Rating         float64 `json:"rating"`
	ReviewsCount   int `json:"reviewsCount"`
	Reviews        []Review `json:"reviews"`
}

type PlantPassport struct {
	Origin string `json:"origin"`; Lighting string `json:"lighting"`; Watering string `json:"watering"`
	Humidity string `json:"humidity"`; Temperature string `json:"temperature"`; Soil string `json:"soil"`
	Fertilizer string `json:"fertilizer"`; Repotting string `json:"repotting"`; CareDifficulty string `json:"careDifficulty"`
	GrowthRate string `json:"growthRate"`; MatureSize string `json:"matureSize"`; Toxicity string `json:"toxicity"`
	Problems string `json:"problems"`; Pests string `json:"pests"`; FAQ []FAQItem `json:"faq"`
}
type FAQItem struct { Question string `json:"question"`; Answer string `json:"answer"` }
type Review struct { ID int64 `json:"id"`; Rating int `json:"rating"`; Text string `json:"text"`; Author string `json:"author"`; Date string `json:"date"`; VerifiedPurchase bool `json:"verifiedPurchase"`; Photos []string `json:"photos"` }

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
