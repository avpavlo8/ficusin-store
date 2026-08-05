// Package payment decides how an order may be paid for and keeps the
// record of what was actually paid.
package payment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The three ways to pay. Which of them a given customer sees is decided by
// Methods, never by the browser.
const (
	MethodOnline     = "online"
	MethodOnDelivery = "on_delivery"
	MethodInvoice    = "invoice"
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
			Title: "При получении",
			Note:  "Оплатите, когда заберёте заказ",
		})
	}
	if wholesaleApproved {
		methods = append(methods, Method{
			ID:    MethodInvoice,
			Title: "По счёту",
			Note:  "Менеджер выставит счёт на организацию",
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

// InitialStatus is what the order starts with once the method is known.
func InitialStatus(method string) string {
	switch method {
	case MethodOnDelivery:
		return StatusOnDelivery
	case MethodInvoice:
		return StatusInvoice
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
}

func NewService(pool *pgxpool.Pool, client provider, returnURL string, logger *slog.Logger) *Service {
	return &Service{pool: pool, provider: client, returnURL: returnURL, logger: logger}
}

func (service *Service) Configured() bool {
	return service != nil && service.provider != nil && service.provider.Configured()
}

// Start creates a payment for an order and returns the page to send the
// customer to. The row is written before YooKassa is called, so a payment
// we started is never invisible to us.
func (service *Service) Start(ctx context.Context, orderNumber string) (string, error) {
	if !service.Configured() {
		return "", errors.New("оплата картой временно недоступна")
	}
	var (
		orderID     int64
		amount      float64
		feePending  int
		status      string
		email       string
		phone       string
		method      string
		existingURL string
	)
	err := service.pool.QueryRow(ctx, `
		SELECT o.id, o.total::DOUBLE PRECISION, o.delivery_fee_pending,
			o.payment_status, COALESCE(o.email, ''), o.phone, o.payment_method,
			COALESCE((
				SELECT p.confirmation_url FROM payments p
				WHERE p.order_id = o.id AND p.status = 'pending'
				ORDER BY p.id DESC LIMIT 1
			), '')
		FROM orders o
		WHERE o.order_number = $1
	`, orderNumber).Scan(
		&orderID, &amount, &feePending, &status, &email, &phone, &method, &existingURL,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errors.New("заказ не найден")
	}
	if err != nil {
		return "", fmt.Errorf("load order for payment: %w", err)
	}
	if status == StatusPaid {
		return "", errors.New("заказ уже оплачен")
	}
	if feePending == 1 {
		return "", errors.New("стоимость доставки ещё не рассчитана — менеджер пришлёт ссылку на оплату")
	}
	// A customer who clicked away from the payment page and came back gets
	// the same page, not a second charge waiting to happen.
	if existingURL != "" {
		return existingURL, nil
	}

	items, err := service.orderItems(ctx, orderID)
	if err != nil {
		return "", err
	}
	// Delivery is a line of the receipt too. Without it the receipt would
	// total less than the payment, and the tax office reads both.
	var deliveryFee float64
	if err := service.pool.QueryRow(ctx, `
		SELECT delivery_fee::DOUBLE PRECISION FROM orders WHERE id = $1
	`, orderID).Scan(&deliveryFee); err == nil && deliveryFee > 0 {
		items = append(items, integration.PaymentItem{
			Name: "Доставка", Price: deliveryFee, Quantity: 1,
		})
	}
	key, err := idempotenceKey()
	if err != nil {
		return "", err
	}
	var paymentID int64
	if err := service.pool.QueryRow(ctx, `
		INSERT INTO payments (order_id, idempotence_key, amount)
		VALUES ($1, $2, $3) RETURNING id
	`, orderID, key, amount).Scan(&paymentID); err != nil {
		return "", fmt.Errorf("insert payment: %w", err)
	}

	created, err := service.provider.CreatePayment(ctx, integration.PaymentRequest{
		IdempotenceKey: key,
		Amount:         amount,
		Description:    "Заказ " + orderNumber + " — Фикусин",
		// Back to the shop with the order number in the query, not to a
		// per-order page: a guest has no account to look at, and a page
		// showing an order to anyone who knows its number would leak.
		ReturnURL: service.returnURL + "/?paid=" + orderNumber,
		Email:          email,
		Phone:          phone,
		Items:          items,
	})
	if err != nil {
		if _, failed := service.pool.Exec(ctx, `
			UPDATE payments SET status = 'cancelled', updated_at = CURRENT_TIMESTAMP
			WHERE id = $1
		`, paymentID); failed != nil {
			service.logger.Error("mark payment failed", "error", failed)
		}
		return "", err
	}
	if _, err := service.pool.Exec(ctx, `
		UPDATE payments
		SET provider_payment_id = $2, status = $3, confirmation_url = $4,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
	`, paymentID, created.ID, created.Status, created.ConfirmationURL); err != nil {
		return "", fmt.Errorf("update payment: %w", err)
	}
	return created.ConfirmationURL, nil
}

// Sync asks the provider what happened to a payment and records the answer.
// Notifications arrive unsigned, so this is the only thing that may mark an
// order paid — the notification merely tells us when to look.
func (service *Service) Sync(ctx context.Context, providerPaymentID string) error {
	if !service.Configured() {
		return errors.New("оплата не настроена")
	}
	payment, err := service.provider.FetchPayment(ctx, providerPaymentID)
	if err != nil {
		return err
	}
	var orderID int64
	var expected float64
	if err := service.pool.QueryRow(ctx, `
		SELECT order_id, amount::DOUBLE PRECISION FROM payments
		WHERE provider_payment_id = $1
	`, providerPaymentID).Scan(&orderID, &expected); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Someone else's payment, or one we never started. Not ours to
			// act on, and not an error worth retrying.
			service.logger.Warn("unknown payment notified", "payment_id", providerPaymentID)
			return nil
		}
		return fmt.Errorf("load payment: %w", err)
	}
	paid := payment.Paid && payment.Status == "succeeded"
	// An amount that does not match what we asked for is never treated as
	// payment for this order, whatever the provider says about it.
	if paid && payment.Amount+0.01 < expected {
		service.logger.Error(
			"payment amount below order total",
			"payment_id", providerPaymentID, "paid", payment.Amount, "expected", expected,
		)
		paid = false
	}
	status := payment.Status
	if paid {
		status = StatusPaid
	}
	transaction, err := service.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	var paidAt any
	if paid {
		paidAt = time.Now()
	}
	if _, err := transaction.Exec(ctx, `
		UPDATE payments SET status = $2, paid_at = $3, updated_at = CURRENT_TIMESTAMP
		WHERE provider_payment_id = $1
	`, providerPaymentID, status, paidAt); err != nil {
		return fmt.Errorf("update payment status: %w", err)
	}
	if paid {
		if _, err := transaction.Exec(ctx, `
			UPDATE orders
			SET payment_status = $2, paid_at = COALESCE(paid_at, CURRENT_TIMESTAMP)
			WHERE id = $1
		`, orderID, StatusPaid); err != nil {
			return fmt.Errorf("mark order paid: %w", err)
		}
	}
	return transaction.Commit(ctx)
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
