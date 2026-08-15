package admin

import (
	"context"
	"errors"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/catalog"
)

const (
	RoleOwner   = "owner"
	RoleManager = "manager"
)

const (
	PermissionDashboard        = "dashboard.read"
	PermissionCustomersRead    = "customers.read"
	PermissionCustomersEdit    = "customers.edit"
	PermissionRolesEdit        = "roles.edit"
	PermissionDiscountsEdit    = "discounts.edit"
	PermissionOrdersRead       = "orders.read"
	PermissionOrdersEdit       = "orders.edit"
	PermissionProductsRead     = "products.read"
	PermissionProductsEdit     = "products.edit"
	PermissionProductsSync     = "products.sync"
	PermissionProcurementRead  = "procurement.read"
	PermissionProcurementEdit  = "procurement.edit"
	PermissionIntegrationsEdit = "integrations.edit"
)

var ErrForbidden = errors.New("admin action is forbidden")
var ErrCategoryNotEmpty = errors.New("category is not empty")

func ValidRole(role string) bool {
	switch role {
	case "", RoleOwner, RoleManager:
		return true
	default:
		return false
	}
}

// AssignableRole deliberately excludes owner. Ownership is configured only
// through ADMIN_EMAILS and cannot be granted from the admin UI or API.
func AssignableRole(role string) bool {
	return role == "" || role == RoleManager
}

func Can(role, permission string) bool {
	if role == RoleOwner {
		return true
	}
	switch role {
	case RoleManager:
		return permission == PermissionDashboard ||
			permission == PermissionCustomersRead ||
			permission == PermissionOrdersRead || permission == PermissionOrdersEdit ||
			permission == PermissionProductsRead || permission == PermissionProductsEdit ||
			permission == PermissionProductsSync ||
			permission == PermissionProcurementRead || permission == PermissionProcurementEdit
	default:
		return false
	}
}

type Dashboard struct {
	Products         int           `json:"products"`
	Variants         int           `json:"variants"`
	Orders           int           `json:"orders"`
	Customers        int           `json:"customers"`
	WholesalePending int           `json:"wholesalePending"`
	LastSync         *SyncRun      `json:"lastSync"`
	RecentOrders     []RecentOrder `json:"recentOrders"`
}

type SyncRun struct {
	Status       string    `json:"status"`
	StartedAt    time.Time `json:"startedAt"`
	ItemsUpdated int       `json:"itemsUpdated"`
	ErrorsCount  int       `json:"errorsCount"`
}

type RecentOrder struct {
	OrderNumber  string    `json:"orderNumber"`
	CustomerName string    `json:"customerName"`
	Total        float64   `json:"total"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"createdAt"`
}

type Actor struct {
	CustomerID int64
	Role       string
}

type Customer struct {
	ID                int64     `json:"id"`
	Email             string    `json:"email"`
	Phone             string    `json:"phone"`
	FullName          string    `json:"fullName"`
	LastName          string    `json:"lastName"`
	Patronymic        string    `json:"patronymic"`
	DeliveryAddress   string    `json:"deliveryAddress"`
	AccountType       string    `json:"accountType"`
	WholesaleStatus   string    `json:"wholesaleStatus"`
	RetailDiscountBPS int       `json:"retailDiscountBps"`
	LifetimeSpend     float64   `json:"lifetimeSpend"`
	Active            bool      `json:"active"`
	AdminRole         string    `json:"adminRole"`
	OrdersCount       int       `json:"ordersCount"`
	CreatedAt         time.Time `json:"createdAt"`
}

type CustomerUpdate struct {
	FullName          *string `json:"fullName"`
	LastName          *string `json:"lastName"`
	Patronymic        *string `json:"patronymic"`
	Email             *string `json:"email"`
	DeliveryAddress   *string `json:"deliveryAddress"`
	AccountType       *string `json:"accountType"`
	WholesaleStatus   *string `json:"wholesaleStatus"`
	RetailDiscountBPS *int    `json:"retailDiscountBps"`
	Active            *bool   `json:"active"`
	AdminRole         *string `json:"adminRole"`
}

type Order struct {
	ID             int64       `json:"id"`
	OrderNumber    string      `json:"orderNumber"`
	CustomerID     *int64      `json:"customerId"`
	CustomerName   string      `json:"customerName"`
	Phone          string      `json:"phone"`
	Email          string      `json:"email"`
	Address        string      `json:"address"`
	Comment        string      `json:"comment"`
	DeliveryMethod string      `json:"deliveryMethod"`
	// DeliveryFeePending marks an order whose delivery price the manager
	// still has to work out — no box dimensions, CDEK unavailable, or the
	// customer asked whether the plants fit into one box.
	DeliveryFeePending bool `json:"deliveryFeePending"`
	RepackRequested    bool `json:"repackRequested"`
	PaymentMethod  string      `json:"paymentMethod"`
	TrackNumber    string      `json:"trackNumber"`
	HasPreorder    bool        `json:"hasPreorder"`
	PaymentStatus  string      `json:"paymentStatus"`
	Status         string      `json:"status"`
	Total          float64     `json:"total"`
	CreatedAt      time.Time   `json:"createdAt"`
	Items          []OrderItem `json:"items"`
}

type OrderItem struct {
	ProductID   string  `json:"productId"`
	ProductName string  `json:"productName"`
	UnitPrice   float64 `json:"unitPrice"`
	Quantity    int     `json:"quantity"`
}

type Product struct {
	ID                 int64      `json:"id"`
	SabyID             string     `json:"sabyId"`
	Slug               string     `json:"slug"`
	Name               string     `json:"name"`
	LatinName          string     `json:"latinName"`
	ShortDescription   string     `json:"shortDescription"`
	Description        string     `json:"description"`
	CareInstructions   string     `json:"careInstructions"`
	Status             string     `json:"status"`
	Featured           bool       `json:"featured"`
	CatalogSection     string     `json:"catalogSection"`
	PlantKind          string     `json:"plantKind"`
	LightLevel         string     `json:"lightLevel"`
	Watering           string     `json:"watering"`
	HeightClass        string     `json:"heightClass"`
	CareLevel          string     `json:"careLevel"`
	Placement          string     `json:"placement"`
	PetSafety          string     `json:"petSafety"`
	GrowthHabit        string     `json:"growthHabit"`
	Image              string     `json:"image"`
	Price              float64    `json:"price"`
	Stock              int        `json:"stock"`
	SKU                string     `json:"sku"`
	VariantLabel       string     `json:"variantLabel"`
	HeightCM           *int       `json:"heightCm"`
	PotDiameterCM      *int       `json:"potDiameterCm"`
	PackageLengthCM    *int       `json:"packageLengthCm"`
	PackageWidthCM     *int       `json:"packageWidthCm"`
	PackageHeightCM    *int       `json:"packageHeightCm"`
	PackageWeightGrams *int       `json:"packageWeightGrams"`
	WholesaleMinQty    int        `json:"wholesaleMinQty"`
	OverrideFields     []string   `json:"overrideFields"`
	// SabyFields — что этому товару разрешено брать из СБИС. Пусто значит
	// «ничего»: карточка целиком наша.
	SabyFields         []string   `json:"sabyFields"`
	SabyCode           string     `json:"sabyCode"`
	SabyUpdatedAt      *time.Time `json:"sabyUpdatedAt"`
	CategoryID         *int64               `json:"categoryId"`
	Passport           catalog.PlantPassport `json:"passport"`
	ImportantWarnings  []string             `json:"importantWarnings"`
	ExternalIDs        []ExternalID          `json:"externalIds"`
	Attributes         map[string]any        `json:"attributes"`
}

type ExternalID struct {
	Provider   string `json:"provider"`
	Type       string `json:"type"`
	ExternalID string `json:"externalId"`
}

type ProductUpdate struct {
	Name               *string `json:"name"`
	LatinName          *string `json:"latinName"`
	ShortDescription   *string `json:"shortDescription"`
	Description        *string `json:"description"`
	CareInstructions   *string `json:"careInstructions"`
	Status             *string `json:"status"`
	Featured           *bool   `json:"featured"`
	CatalogSection     *string `json:"catalogSection"`
	PlantKind          *string `json:"plantKind"`
	LightLevel         *string `json:"lightLevel"`
	Watering           *string `json:"watering"`
	HeightClass        *string `json:"heightClass"`
	CareLevel          *string `json:"careLevel"`
	Placement          *string `json:"placement"`
	PetSafety          *string `json:"petSafety"`
	GrowthHabit        *string `json:"growthHabit"`
	Image              *string `json:"image"`
	PriceMinor         *int64  `json:"priceMinor"`
	VariantLabel       *string `json:"variantLabel"`
	HeightCM           *int    `json:"heightCm"`
	PotDiameterCM      *int    `json:"potDiameterCm"`
	PackageLengthCM    *int    `json:"packageLengthCm"`
	PackageWidthCM     *int    `json:"packageWidthCm"`
	PackageHeightCM    *int    `json:"packageHeightCm"`
	PackageWeightGrams *int    `json:"packageWeightGrams"`
	WholesaleMinQty    *int    `json:"wholesaleMinQty"`
	Stock              *int    `json:"stock"`
	SabyFields         *[]string `json:"sabyFields"`
	CategoryID         *int64                `json:"categoryId"`
	Passport           *catalog.PlantPassport `json:"passport"`
	ImportantWarnings  *[]string             `json:"importantWarnings"`
	Attributes         map[string]any         `json:"attributes"`
}

// ProductCreate — карточка, заведённая в магазине с нуля.
type ProductCreate struct {
	Name             string `json:"name"`
	LatinName        string `json:"latinName"`
	ShortDescription string `json:"shortDescription"`
	Description      string `json:"description"`
	CatalogSection   string `json:"catalogSection"`
	CategoryID       *int64 `json:"categoryId"`
	PriceMinor       int64  `json:"priceMinor"`
	Stock            int    `json:"stock"`
	Image            string `json:"image"`
	HeightCM         *int `json:"heightCm"`
	PotDiameterCM    *int `json:"potDiameterCm"`
	PackageLengthCM  *int `json:"packageLengthCm"`
	PackageWidthCM   *int `json:"packageWidthCm"`
	PackageHeightCM  *int `json:"packageHeightCm"`
	PackageWeightGrams *int `json:"packageWeightGrams"`
	Attributes       map[string]any `json:"attributes"`
}

// ImportRequest — массовый импорт из справочника СБИС по кодам товаров.
// DryRun показывает, что произойдёт, ничего не создавая.
type ImportRequest struct {
	Codes      []string `json:"codes"`
	CategoryID *int64   `json:"categoryId"`
	DryRun     bool     `json:"dryRun"`
}

// ImportEntry — судьба одного кода: заведён, уже был или не найден.
type ImportEntry struct {
	Code      string  `json:"code"`
	Status    string  `json:"status"`
	Name      string  `json:"name"`
	Price     float64 `json:"price"`
	Stock     int     `json:"stock"`
	ProductID *int64  `json:"productId"`
	Slug      string  `json:"slug"`
}

type ImportResult struct {
	Created int           `json:"created"`
	Entries []ImportEntry `json:"entries"`
}

type Category struct {
	ID            int64  `json:"id"`
	ParentID      *int64 `json:"parentId"`
	Name          string `json:"name"`
	Slug          string `json:"slug"`
	SortOrder     int    `json:"sortOrder"`
	Icon          string `json:"icon"`
	ProductsCount int    `json:"productsCount"`
	ChildrenCount int    `json:"childrenCount"`
}

type CategoryCreate struct {
	ParentID  *int64 `json:"parentId"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	SortOrder int    `json:"sortOrder"`
}

type CategoryUpdate struct {
	Name      *string `json:"name"`
	Slug      *string `json:"slug"`
	SortOrder *int    `json:"sortOrder"`
}

// CategoryAttribute is the product-editor contract for one category. Audience
// keeps delivery/integration fields out of the customer-facing PDP contract.
type CategoryAttribute struct {
	Code         string `json:"code"`
	Name         string `json:"name"`
	DataType     string `json:"dataType"`
	Unit         string `json:"unit"`
	Options      []string `json:"options"`
	Audience     string `json:"audience"`
	Required     bool `json:"required"`
	Filterable   bool `json:"filterable"`
	ShowOnPDP    bool `json:"showOnPdp"`
	Badge        bool `json:"badge"`
	SortOrder    int `json:"sortOrder"`
}

type SyncRequest struct {
	ProductIDs []int64  `json:"productIds"`
	Fields     []string `json:"fields"`
}

type SyncResult struct {
	Updated int     `json:"updated"`
	Skipped []int64 `json:"skipped"`
}

type Repository interface {
	Dashboard(context.Context) (Dashboard, error)
	ListCustomers(context.Context) ([]Customer, error)
	UpdateCustomer(context.Context, Actor, int64, CustomerUpdate) (Customer, error)
	ListOrders(context.Context) ([]Order, error)
	UpdateOrderStatus(context.Context, Actor, int64, string, string) (Order, error)
	ListProducts(context.Context) ([]Product, error)
	UpdateProduct(context.Context, Actor, int64, ProductUpdate) (Product, error)
	CreateProduct(context.Context, Actor, ProductCreate) (Product, error)
	ImportProducts(context.Context, Actor, ImportRequest) (ImportResult, error)
	SyncProducts(context.Context, Actor, SyncRequest) (SyncResult, error)
	ListCategories(context.Context) ([]Category, error)
	CreateCategory(context.Context, Actor, CategoryCreate) (Category, error)
	UpdateCategory(context.Context, Actor, int64, CategoryUpdate) (Category, error)
	DeleteCategory(context.Context, Actor, int64) error
}
