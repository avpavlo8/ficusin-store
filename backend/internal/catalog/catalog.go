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
	Icon      string `json:"icon"`
}

// Product is one catalogue card (SPU). ID is the Ficusin product code used in
// /product/{id}; SKU is the default sellable variant shown in catalogue cards.
type Product struct {
	ID               string             `json:"id"`
	SKU              string             `json:"sku"`
	Name             string             `json:"name"`
	Latin            string             `json:"latin"`
	Category         string             `json:"category"`
	Price            float64            `json:"price"`
	Image            string             `json:"image"`
	Light            string             `json:"light"`
	Size             string             `json:"size"`
	Stock            int                `json:"stock"`
	CatalogSection   string             `json:"catalogSection"`
	PlantKind        string             `json:"plantKind,omitempty"`
	LightLevel       string             `json:"lightLevel,omitempty"`
	Watering         string             `json:"watering,omitempty"`
	HeightClass      string             `json:"heightClass,omitempty"`
	CareLevel        string             `json:"careLevel,omitempty"`
	Placement        string             `json:"placement,omitempty"`
	PetSafety        string             `json:"petSafety,omitempty"`
	GrowthHabit      string             `json:"growthHabit,omitempty"`
	CategoryID       *int64             `json:"categoryId,omitempty"`
	Collections      []string           `json:"collections"`
	Rating           float64            `json:"rating"`
	ReviewsCount     int                `json:"reviewsCount"`
	PopularityScore  float64            `json:"popularityScore"`
	FilterAttributes []ProductAttribute `json:"filterAttributes"`
}

type Collection struct {
	Slug     string `json:"slug"`
	Title    string `json:"title"`
	Note     string `json:"note"`
	CoverURL string `json:"coverUrl"`
	Count    int    `json:"count"`
}

// FeedOffer is one sellable SKU exported to search engines. Unlike a catalogue
// card it never collapses sizes: price, stock and the landing URL must describe
// the exact variant a search result advertises.
type FeedOffer struct {
	ProductCode  string
	SKU          string
	Name         string
	Label        string
	Description  string
	Price        float64
	Stock        int
	Image        string
	CategoryID   int64
	Category     string
	VariantCount int
}

type ProductDetail struct {
	ID                string        `json:"id"`
	Name              string        `json:"name"`
	Latin             string        `json:"latin"`
	ShortDescription  string        `json:"shortDescription"`
	Description       string        `json:"description"`
	CareInstructions  string        `json:"careInstructions"`
	Images            []string      `json:"images"`
	Variants          []Variant     `json:"variants"`
	Recommendations   []Product     `json:"recommendations"`
	CatalogSection    string        `json:"catalogSection"`
	PlantKind         string        `json:"plantKind,omitempty"`
	LightLevel        string        `json:"lightLevel,omitempty"`
	Watering          string        `json:"watering,omitempty"`
	HeightClass       string        `json:"heightClass,omitempty"`
	CareLevel         string        `json:"careLevel,omitempty"`
	Placement         string        `json:"placement,omitempty"`
	PetSafety         string        `json:"petSafety,omitempty"`
	GrowthHabit       string        `json:"growthHabit,omitempty"`
	CategoryID        *int64        `json:"categoryId,omitempty"`
	Passport          PlantPassport `json:"passport"`
	ImportantWarnings []string      `json:"importantWarnings"`
	Rating            float64       `json:"rating"`
	ReviewsCount      int           `json:"reviewsCount"`
	Reviews           []Review      `json:"reviews"`
	// Attributes contains customer-visible values shared by the whole card.
	Attributes []ProductAttribute `json:"attributes"`
}

// ProductAttribute is always safe for the customer contract. Technical
// attributes never leave the catalogue API. Variant-scoped attributes live on
// Variant.Attributes so switching SKU cannot display another size's values.
type ProductAttribute struct {
	Code                  string            `json:"code"`
	Name                  string            `json:"name"`
	Unit                  string            `json:"unit,omitempty"`
	Value                 any               `json:"value"`
	DisplayValue          any               `json:"displayValue"`
	Options               []string          `json:"options"`
	OptionLabels          map[string]string `json:"optionLabels"`
	DataType              string            `json:"-"`
	DisplayMode           string            `json:"displayMode,omitempty"`
	Badge                 bool              `json:"badge"`
	Filterable            bool              `json:"filterable"`
	SummaryPosition       *int              `json:"summaryPosition,omitempty"`
	ShowInCharacteristics bool              `json:"showInCharacteristics"`
}

// localizeAttributeValue keeps stable machine values intact and derives the
// customer-facing representation exclusively from attribute_options. Unknown
// codes deliberately fall back to themselves so legacy data cannot break the
// public response while the missing dictionary entry is repaired.
func localizeAttributeValue(attribute *ProductAttribute) {
	if attribute.Options == nil {
		attribute.Options = []string{}
	}
	if attribute.OptionLabels == nil {
		attribute.OptionLabels = map[string]string{}
	}
	localize := func(value any) any {
		switch typed := value.(type) {
		case bool:
			if typed {
				return "Есть"
			}
			return "Нет"
		case string:
			if label, ok := attribute.OptionLabels[typed]; ok && label != "" {
				return label
			}
			return typed
		default:
			return value
		}
	}
	if values, ok := attribute.Value.([]any); ok {
		labels := make([]string, 0, len(values))
		for _, value := range values {
			label, ok := localize(value).(string)
			if !ok {
				continue
			}
			labels = append(labels, label)
		}
		attribute.DisplayValue = labels
		return
	}
	attribute.DisplayValue = localize(attribute.Value)
}

type PlantPassport struct {
	Origin         string    `json:"origin"`
	Lighting       string    `json:"lighting"`
	Watering       string    `json:"watering"`
	Humidity       string    `json:"humidity"`
	Temperature    string    `json:"temperature"`
	Soil           string    `json:"soil"`
	Fertilizer     string    `json:"fertilizer"`
	Repotting      string    `json:"repotting"`
	CareDifficulty string    `json:"careDifficulty"`
	GrowthRate     string    `json:"growthRate"`
	MatureSize     string    `json:"matureSize"`
	Toxicity       string    `json:"toxicity"`
	Problems       string    `json:"problems"`
	Pests          string    `json:"pests"`
	FAQ            []FAQItem `json:"faq"`
}
type FAQItem struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}
type ReviewMedia struct {
	URL         string `json:"url"`
	ContentType string `json:"contentType"`
}
type Review struct {
	ID               int64         `json:"id"`
	Rating           int           `json:"rating"`
	Text             string        `json:"text"`
	Author           string        `json:"author"`
	Date             string        `json:"date"`
	VerifiedPurchase bool          `json:"verifiedPurchase"`
	Photos           []string      `json:"photos"`
	Media            []ReviewMedia `json:"media"`
}

type Variant struct {
	ID              int64              `json:"id"`
	SKU             string             `json:"sku"`
	Label           string             `json:"label"`
	Price           float64            `json:"price"`
	Stock           int                `json:"stock"`
	HeightCM        *int               `json:"heightCm"`
	PotDiameterCM   *int               `json:"potDiameterCm"`
	WholesaleMinQty int                `json:"wholesaleMinQty"`
	Images          []string           `json:"images"`
	Attributes      []ProductAttribute `json:"attributes"`
}

type Repository interface {
	ListAvailable(context.Context) ([]Product, error)
	ListCategories(context.Context) ([]Category, error)
	DetailBySlug(context.Context, string) (ProductDetail, error)
}
