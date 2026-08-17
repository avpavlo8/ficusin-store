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
	Refund(ctx context.Context, paymentID string, amount float64, idempotenceKey string) error
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
		orderStatus string
		email       string
		phone       string
		method      string
		existingURL string
		paymentID   int64
		key         string
	)
	err := service.pool.QueryRow(ctx, `
		SELECT o.id, o.total::DOUBLE PRECISION, o.delivery_fee_pending,
			o.payment_status, o.status, COALESCE(o.email, ''), o.phone, o.payment_method,
			COALESCE((
			SELECT p.confirmation_url FROM payments p
				WHERE p.order_id = o.id AND p.status = 'pending'
				ORDER BY p.id DESC LIMIT 1
			), '')
		FROM orders o
		WHERE o.order_number = $1
	`, orderNumber).Scan(
		&orderID, &amount, &feePending, &status, &orderStatus, &email, &phone, &method, &existingURL,
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
	if orderStatus == "cancelled" || orderStatus == "completed" {
		return "", errors.New("этот заказ уже закрыт")
	}
	if method != MethodOnline {
		return "", errors.New("для заказа выбран другой способ оплаты")
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
	key, err = idempotenceKey()
	if err != nil {
		return "", err
	}
	err = service.pool.QueryRow(ctx, `
		INSERT INTO payments (order_id, idempotence_key, amount)
		VALUES ($1, $2, $3)
		ON CONFLICT (order_id) WHERE status = 'pending' DO NOTHING
		RETURNING id, idempotence_key
	`, orderID, key, amount).Scan(&paymentID, &key)
	if errors.Is(err, pgx.ErrNoRows) {
		// Another request won the race. Reuse its durable idempotence key:
		// YooKassa will then return the same payment instead of charging twice.
		err = service.pool.QueryRow(ctx, `
			SELECT id, idempotence_key, confirmation_url
			FROM payments WHERE order_id = $1 AND status = 'pending'
			ORDER BY id DESC LIMIT 1
		`, orderID).Scan(&paymentID, &key, &existingURL)
		if err == nil && existingURL != "" {
			return existingURL, nil
		}
	}
	if err != nil {
		return "", fmt.Errorf("insert or load payment: %w", err)
	}

	created, err := service.provider.CreatePayment(ctx, integration.PaymentRequest{
		IdempotenceKey: key,
		Amount:         amount,
		Description:    "Заказ " + orderNumber + " — Фикусин",
		// Back to the shop with the order number in the query, not to a
		// per-order page: a guest has no account to look at, and a page
		// showing an order to anyone who knows its number would leak.
		ReturnURL: service.returnURL + "/?paid=" + orderNumber,
		Email:     email,
		Phone:     phone,
		Items:     items,
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
	var orderNumber string
	var orderStatus string
	if err := service.pool.QueryRow(ctx, `
		SELECT p.order_id, p.amount::DOUBLE PRECISION, o.order_number, o.status
		FROM payments p
		JOIN orders o ON o.id = p.order_id
		WHERE p.provider_payment_id = $1
	`, providerPaymentID).Scan(&orderID, &expected, &orderNumber, &orderStatus); err != nil {
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
	// Деньги за отменённый заказ. Товар уже вернулся на полку, а платёж
	// всё-таки прошёл — значит его не успели закрыть у провайдера.
	//
	// Факт оплаты записываем: деньги реальны, и делать вид, что их нет,
	// хуже всего. Но заказ остаётся отменённым, и это громкая ошибка, а не
	// тишина: дальше нужен человек, который вернёт деньги или восстановит
	// заказ. Тихо «оплатить» отменённый заказ нельзя — товара под него уже
	// нет, его мог купить кто-то другой.
	if paid && orderStatus == "cancelled" {
		service.logger.Error(
			"оплата пришла за отменённый заказ: нужен возврат или восстановление вручную",
			"order_number", orderNumber,
			"order_id", orderID,
			"payment_id", providerPaymentID,
			"amount", payment.Amount,
		)
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

// Refund sends the customer's money back for an order that will not happen.
// It is deliberately callable only for an order that really was paid: the
// panel offers the button, but the decision that money moved is made here.
func (service *Service) Refund(ctx context.Context, orderID int64) error {
	if !service.Configured() {
		return errors.New("возврат недоступен: оплата не настроена")
	}
	var providerPaymentID string
	var amount float64
	err := service.pool.QueryRow(ctx, `
		SELECT p.provider_payment_id, p.amount::DOUBLE PRECISION
		FROM payments p
		JOIN orders o ON o.id = p.order_id
		WHERE p.order_id = $1 AND p.status = $2 AND o.payment_status = $2
		ORDER BY p.id DESC
		LIMIT 1
	`, orderID, StatusPaid).Scan(&providerPaymentID, &amount)
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("по этому заказу нет оплаченного платежа")
	}
	if err != nil {
		return fmt.Errorf("load payment for refund: %w", err)
	}
	key, err := idempotenceKey()
	if err != nil {
		return err
	}
	if err := service.provider.Refund(ctx, providerPaymentID, amount, key); err != nil {
		return err
	}
	// Recorded only after the provider confirms: an order marked refunded
	// while the money is still with us is worse than one marked late.
	if _, err := service.pool.Exec(ctx, `
		UPDATE payments SET status = 'refunded', updated_at = CURRENT_TIMESTAMP
		WHERE order_id = $1 AND status = $2
	`, orderID, StatusPaid); err != nil {
		return fmt.Errorf("mark payment refunded: %w", err)
	}
	if _, err := service.pool.Exec(ctx, `
		UPDATE orders SET payment_status = 'refunded' WHERE id = $1
	`, orderID); err != nil {
		return fmt.Errorf("mark order refunded: %w", err)
	}
	return nil
}
