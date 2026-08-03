package admin

import (
	"context"
	"errors"
	"time"
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
	PermissionIntegrationsEdit = "integrations.edit"
)

var ErrForbidden = errors.New("admin action is forbidden")

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
			permission == PermissionProductsSync
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
	SabyUpdatedAt      *time.Time `json:"sabyUpdatedAt"`
}

type ProductUpdate struct {
	Name               *string `json:"name"`
	LatinName          *string `json:"latinName"`
	ShortDescription   *string `json:"shortDescription"`
	Description        *string `json:"description"`
	CareInstructions   *string `json:"careInstructions"`
	Status             *string `json:"status"`
	Featured           *bool   `json:"featured"`
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
	SyncProducts(context.Context, Actor, SyncRequest) (SyncResult, error)
}
