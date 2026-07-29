package order

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type CDEK interface {
	GetOffices(context.Context, int) ([]integration.CDEKOffice, error)
	CalculatePVZ(context.Context, int, int) (integration.CDEKQuote, error)
}

type Notifier interface {
	SendOrder(context.Context, integration.TelegramOrder) error
}

type Service struct {
	pool     *pgxpool.Pool
	cdek     CDEK
	notifier Notifier
	logger   *slog.Logger
}

type CreateInput struct {
	Customer   CustomerInput
	Delivery   string
	Items      []ItemInput
	CDEK       CDEKInput
	CustomerID *int64
}

type CustomerInput struct {
	Name    string
	Phone   string
	Email   string
	Address string
	Comment string
}

type ItemInput struct {
	ID       string
	Quantity int
}

type CDEKInput struct {
	CityCode   int
	CityName   string
	OfficeCode string
}

type Created struct {
	OrderNumber   string `json:"orderNumber"`
	PaymentStatus string `json:"paymentStatus"`
}

type ValidationError struct {
	Message string
}

func (err *ValidationError) Error() string {
	return err.Message
}

func invalid(message string) error {
	return &ValidationError{Message: message}
}

type purchasableItem struct {
	ID       string
	Name     string
	Price    float64
	Quantity int
}

func NewService(
	pool *pgxpool.Pool,
	cdek CDEK,
	notifier Notifier,
	logger *slog.Logger,
) *Service {
	return &Service{pool: pool, cdek: cdek, notifier: notifier, logger: logger}
}

func (service *Service) Create(ctx context.Context, input CreateInput) (Created, error) {
	transaction, err := service.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Created{}, fmt.Errorf("begin order: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	items := make([]purchasableItem, 0, len(input.Items))
	for _, requested := range input.Items {
		var item purchasableItem
		var priceMinor int64
		var stock int
		err := transaction.QueryRow(ctx, `
			SELECT
				p.slug,
				p.name,
				pv.base_price_minor,
				COALESCE(SUM(GREATEST(i.available_qty - i.reserved_qty, 0)), 0)::INTEGER
			FROM products p
			JOIN product_variants pv ON pv.product_id = p.id AND pv.is_active = 1
			LEFT JOIN inventory i ON i.variant_id = pv.id
			WHERE p.slug = $1 AND p.status = 'published'
			GROUP BY p.id, pv.id
			ORDER BY pv.id
			LIMIT 1
		`, requested.ID).Scan(&item.ID, &item.Name, &priceMinor, &stock)
		if errors.Is(err, pgx.ErrNoRows) {
			return Created{}, invalid("Товар больше не доступен. Обновите страницу")
		}
		if err != nil {
			return Created{}, fmt.Errorf("load order product: %w", err)
		}
		item.Quantity = max(1, min(20, requested.Quantity))
		if stock < item.Quantity {
			return Created{}, invalid(fmt.Sprintf("%s: доступно только %d шт.", item.Name, stock))
		}
		item.Price = float64(priceMinor) / 100
		items = append(items, item)
	}
	if len(items) == 0 {
		return Created{}, invalid("Корзина пуста")
	}

	subtotal := 0.0
	itemCount := 0
	for _, item := range items {
		subtotal += item.Price * float64(item.Quantity)
		itemCount += item.Quantity
	}
	deliveryFees := map[string]float64{"pickup": 0, "courier": 490, "post": 590}
	deliveryFee, regularDelivery := deliveryFees[input.Delivery]
	deliveryAddress := input.Customer.Address
	var cityCode, tariffCode *int
	var cityName, officeCode *string
	if input.Delivery == "cdek" {
		if input.CDEK.CityCode <= 0 || strings.TrimSpace(input.CDEK.OfficeCode) == "" {
			return Created{}, invalid("Выберите город и пункт выдачи СДЭК")
		}
		quote, err := service.cdek.CalculatePVZ(ctx, input.CDEK.CityCode, itemCount)
		if err != nil {
			return Created{}, err
		}
		offices, err := service.cdek.GetOffices(ctx, input.CDEK.CityCode)
		if err != nil {
			return Created{}, err
		}
		var selected *integration.CDEKOffice
		for index := range offices {
			if offices[index].Code == input.CDEK.OfficeCode {
				selected = &offices[index]
				break
			}
		}
		if selected == nil {
			return Created{}, invalid("Выбранный пункт СДЭК больше недоступен")
		}
		deliveryFee = float64(quote.Price)
		deliveryAddress = selected.Location.AddressFull
		if deliveryAddress == "" {
			deliveryAddress = selected.Location.Address
		}
		resolvedCityName := selected.Location.City
		if resolvedCityName == "" {
			resolvedCityName = input.CDEK.CityName
		}
		cityCode = &input.CDEK.CityCode
		cityName = &resolvedCityName
		officeCode = &input.CDEK.OfficeCode
		tariffCode = &quote.TariffCode
	} else if !regularDelivery {
		return Created{}, invalid("Выберите способ получения")
	}

	orderNumber, err := newOrderNumber()
	if err != nil {
		return Created{}, err
	}
	total := subtotal + deliveryFee
	var customerID any
	if input.CustomerID != nil {
		customerID = *input.CustomerID
	}
	var orderID int64
	err = transaction.QueryRow(ctx, `
		INSERT INTO orders (
			order_number, customer_id, customer_name, phone, email, address, comment,
			delivery_method, delivery_fee, cdek_city_code, cdek_city_name,
			cdek_office_code, cdek_tariff_code, subtotal, total, payment_status, status
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, $12, $13, $14, $15, 'payment_provider_pending', 'new'
		)
		RETURNING id
	`,
		orderNumber, customerID, input.Customer.Name, input.Customer.Phone,
		input.Customer.Email, deliveryAddress, input.Customer.Comment, input.Delivery,
		deliveryFee, cityCode, cityName, officeCode, tariffCode, subtotal, total,
	).Scan(&orderID)
	if err != nil {
		return Created{}, fmt.Errorf("insert order: %w", err)
	}
	for _, item := range items {
		if _, err := transaction.Exec(ctx, `
			INSERT INTO order_items (
				order_id, product_id, product_name, unit_price, quantity
			) VALUES ($1, $2, $3, $4, $5)
		`, orderID, item.ID, item.Name, item.Price, item.Quantity); err != nil {
			return Created{}, fmt.Errorf("insert order item: %w", err)
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return Created{}, fmt.Errorf("commit order: %w", err)
	}

	notificationItems := make([]integration.TelegramOrderItem, 0, len(items))
	for _, item := range items {
		notificationItems = append(notificationItems, integration.TelegramOrderItem{
			Name: item.Name, Price: item.Price, Quantity: item.Quantity,
		})
	}
	if err := service.notifier.SendOrder(ctx, integration.TelegramOrder{
		OrderNumber: orderNumber, CustomerName: input.Customer.Name,
		Phone: input.Customer.Phone, Email: input.Customer.Email,
		Address: deliveryAddress, Comment: input.Customer.Comment,
		DeliveryMethod: input.Delivery, DeliveryFee: deliveryFee,
		Subtotal: subtotal, Total: total, Items: notificationItems,
	}); err == nil {
		_, _ = service.pool.Exec(ctx, `
			UPDATE orders SET telegram_notified_at = CURRENT_TIMESTAMP WHERE id = $1
		`, orderID)
	} else {
		service.logger.Error(
			"send immediate Telegram order failed; background retry scheduled",
			"order_id", orderID,
			"error", err,
		)
	}

	return Created{OrderNumber: orderNumber, PaymentStatus: "payment_provider_pending"}, nil
}

func newOrderNumber() (string, error) {
	random := make([]byte, 3)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate order number: %w", err)
	}
	return fmt.Sprintf(
		"ZR-%s-%s",
		time.Now().Format("060102"),
		strings.ToUpper(hex.EncodeToString(random)[:5]),
	), nil
}
