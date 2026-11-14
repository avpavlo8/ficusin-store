// Package payment decides how an order may be paid for and keeps the
// record of what was actually paid.
package payment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The three ways to pay. Which of them a given customer sees is decided by
// Methods, never by the browser.
const (
	MethodOnline     = "online"
	MethodOnDelivery = "on_delivery"
	MethodInvoice    = "invoice"
	MethodManager    = "manager_confirmation"
)

const (
	StatusPending    = "pending"
	StatusPaid       = "paid"
	StatusOnDelivery = "on_delivery"
	StatusInvoice    = "invoice"
	StatusCancelled  = "cancelled"
)

// Method is one option as the checkout shows it.
type Method struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Note  string `json:"note"`
}

// Methods returns what this customer may use for this delivery.
//
// Paying at the counter only makes sense when there is a counter: a parcel
// handed to CDEK is gone before anyone could collect the money. An invoice
// is for approved wholesale customers, who pay from a company account and
// need the paperwork.
func Methods(delivery string, wholesaleApproved, onlineConfigured bool) []Method {
	methods := make([]Method, 0, 3)
	if onlineConfigured {
		methods = append(methods, Method{
			ID:    MethodOnline,
			Title: "Картой на сайте",
			Note:  "Оплата сразу после оформления",
		})
	}
	if delivery == "pickup" {
		methods = append(methods, Method{
			ID:    MethodOnDelivery,
			Title: "Наличными при получении",
			Note:  "В магазине при самовывозе",
		})
	}
	if wholesaleApproved {
		methods = append(methods, Method{
			ID:    MethodInvoice,
			Title: "По счёту",
			Note:  "Менеджер выставит счёт на организацию",
		})
	}
	if len(methods) == 0 {
		methods = append(methods, Method{
			ID:    MethodManager,
			Title: "После подтверждения менеджером",
			Note:  "Оплата после подтверждения заказа менеджером",
		})
	}
	return methods
}

// Allowed reports whether the customer may use the method they asked for.
// The browser sends a string; this is what decides it is honest.
func Allowed(method, delivery string, wholesaleApproved, onlineConfigured bool) bool {
	for _, option := range Methods(delivery, wholesaleApproved, onlineConfigured) {
		if option.ID == method {
			return true
		}
	}
	return false
}

// ResolveMethod preserves an available payment choice for preorders too.
// A preorder without a choice keeps its future payment method until confirmation.
func ResolveMethod(requested, delivery string, wholesaleApproved, onlineConfigured, hasPreorder bool) (string, bool) {
	if Allowed(requested, delivery, wholesaleApproved, onlineConfigured) {
		return requested, true
	}
	if hasPreorder {
		if onlineConfigured {
			return MethodOnline, true
		}
		return MethodManager, true
	}
	return "", false
}

// InitialStatus is what the order starts with once the method is known.
func InitialStatus(method string) string {
	switch method {
	case MethodOnDelivery:
		return StatusOnDelivery
	case MethodInvoice:
		return StatusInvoice
	case MethodManager:
		return StatusPending
	default:
		return StatusPending
	}
}

type provider interface {
	Configured() bool
	CreatePayment(context.Context, integration.PaymentRequest) (integration.Payment, error)
	FetchPayment(context.Context, string) (integration.Payment, error)
}

type Service struct {
	pool      *pgxpool.Pool
	provider  provider
	returnURL string
	logger    *slog.Logger
	// A payment operation holds one connection for its advisory lock and can
	// query through the pool on another. Leave capacity for those queries.
	operationSlots chan struct{}
}

func NewService(pool *pgxpool.Pool, client provider, returnURL string, logger *slog.Logger) *Service {
	return &Service{pool: pool, provider: client, returnURL: returnURL, logger: logger, operationSlots: make(chan struct{}, 2)}
}

func (service *Service) Configured() bool {
	return service != nil && service.provider != nil && service.provider.Configured()
}

func (service *Service) orderItems(ctx context.Context, orderID int64) ([]integration.PaymentItem, error) {
	rows, err := service.pool.Query(ctx, `
		SELECT product_name, unit_price::DOUBLE PRECISION, quantity
		FROM order_items WHERE order_id = $1 ORDER BY id
	`, orderID)
	if err != nil {
		return nil, fmt.Errorf("load payment items: %w", err)
	}
	defer rows.Close()
	items := make([]integration.PaymentItem, 0)
	for rows.Next() {
		var item integration.PaymentItem
		if err := rows.Scan(&item.Name, &item.Price, &item.Quantity); err != nil {
			return nil, fmt.Errorf("scan payment item: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func idempotenceKey() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

// Refund sends the customer's money back for an order that will not happen.
// It is deliberately callable only for an order that really was paid: the
// panel offers the button, but the decision that money moved is made here.
func (service *Service) Refund(ctx context.Context, orderID int64) error {
	balance, err := service.BalanceForOrder(ctx, orderID)
	if err != nil {
		return err
	}
	amount := balance.NetPaid
	if requestID, _ := ctx.Value(refundRequestKey{}).(string); requestID != "" {
		var previous float64
		if err := service.pool.QueryRow(ctx, `SELECT COALESCE(SUM(amount),0)::double precision FROM payment_refunds WHERE order_id=$1 AND request_id=$2`, orderID, requestID).Scan(&previous); err != nil {
			return err
		}
		if previous > 0 {
			amount = previous
		}
	}
	_, err = service.RefundAmount(ctx, orderID, amount, "Полный возврат заказа")
	return err
}
