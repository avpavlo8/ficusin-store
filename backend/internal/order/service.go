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

	"github.com/avpavlo8/ficusin-store/backend/internal/consent"
	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type CDEK interface {
	GetOffices(context.Context, int) ([]integration.CDEKOffice, error)
	CalculatePVZ(context.Context, int, integration.Parcel) ([]integration.CDEKQuote, error)
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
	// Consent is the agreement to the privacy policy and the offer that the
	// checkout form collects. It is recorded alongside the order so the
	// agreement can be evidenced later.
	Consent   bool
	ClientIP  string
	UserAgent string
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
	// TariffCode is the option the customer picked at the checkout. The
	// price is still taken from CDEK's own answer, never from the browser;
	// this only says which of the offered tariffs to charge for. Zero means
	// "the cheapest one", which is what the checkout preselects.
	TariffCode int
	// Repack is the customer asking whether the plants could travel in one
	// box instead of several. Only the person packing them can answer that,
	// so the price waits for the manager.
	Repack bool
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

// boolToInt matches how the rest of the schema stores flags: SMALLINT 0/1.
func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

type purchasableItem struct {
	ID        string
	VariantID int64
	Name      string
	Price     float64
	Quantity  int
	Parcel    integration.Parcel
}

// shippingBox builds the single box the order travels in from the boxes of
// its items. Quantity matters: three identical plants are three boxes side
// by side, not one.
func shippingBox(items []purchasableItem) (integration.Parcel, bool) {
	parcels := make([]integration.Parcel, 0, len(items))
	for _, item := range items {
		for count := 0; count < item.Quantity; count++ {
			parcels = append(parcels, item.Parcel)
		}
	}
	return integration.CombineParcels(parcels)
}

// mergeItems collapses repeated products into a single line. Without this a
// cart holding the same plant twice would be checked against stock twice
// with the smaller number each time, and would print two identical rows on
// the order.
func mergeItems(requested []ItemInput) []ItemInput {
	merged := make([]ItemInput, 0, len(requested))
	position := make(map[string]int, len(requested))
	for _, item := range requested {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		if index, seen := position[id]; seen {
			merged[index].Quantity += item.Quantity
			continue
		}
		position[id] = len(merged)
		merged = append(merged, ItemInput{ID: id, Quantity: item.Quantity})
	}
	return merged
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
	if !input.Consent {
		return Created{}, invalid("Подтвердите согласие на обработку персональных данных")
	}

	transaction, err := service.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Created{}, fmt.Errorf("begin order: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	requestedItems := mergeItems(input.Items)
	items := make([]purchasableItem, 0, len(requestedItems))
	for _, requested := range requestedItems {
		var item purchasableItem
		var priceMinor int64
		err := transaction.QueryRow(ctx, `
			SELECT p.slug, p.name, pv.id, pv.base_price_minor,
				COALESCE(pv.package_length_cm, 0), COALESCE(pv.package_width_cm, 0),
				COALESCE(pv.package_height_cm, 0), COALESCE(pv.package_weight_grams, 0)
			FROM products p
			JOIN product_variants pv ON pv.product_id = p.id AND pv.is_active = 1
			WHERE p.slug = $1 AND p.status = 'published'
			ORDER BY pv.id
			LIMIT 1
		`, requested.ID).Scan(
			&item.ID, &item.Name, &item.VariantID, &priceMinor,
			&item.Parcel.LengthCM, &item.Parcel.WidthCM,
			&item.Parcel.HeightCM, &item.Parcel.WeightGrams,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return Created{}, invalid("Товар больше не доступен. Обновите страницу")
		}
		if err != nil {
			return Created{}, fmt.Errorf("load order product: %w", err)
		}
		item.Quantity = max(1, min(20, requested.Quantity))
		if err := reserveStock(ctx, transaction, item); err != nil {
			return Created{}, err
		}
		item.Price = float64(priceMinor) / 100
		items = append(items, item)
	}
	if len(items) == 0 {
		return Created{}, invalid("Корзина пуста")
	}

	subtotal := 0.0
	for _, item := range items {
		subtotal += item.Price * float64(item.Quantity)
	}
	deliveryFees := map[string]float64{"pickup": 0, "courier": 490, "post": 590}
	deliveryFee, regularDelivery := deliveryFees[input.Delivery]
	deliveryAddress := input.Customer.Address
	var cityCode, tariffCode *int
	var cityName, officeCode *string
	// feePending means the price is left for a person to work out. Nothing
	// about it stops the order: a delivery quote we cannot produce is our
	// problem, and losing the sale over it would be the worse outcome.
	feePending := false
	if input.Delivery == "cdek" {
		if input.CDEK.CityCode <= 0 || strings.TrimSpace(input.CDEK.OfficeCode) == "" {
			return Created{}, invalid("Выберите город и пункт выдачи СДЭК")
		}
		cityCode = &input.CDEK.CityCode
		officeCode = &input.CDEK.OfficeCode
		resolvedCityName := strings.TrimSpace(input.CDEK.CityName)

		// A customer who asked us to pack everything into one box gets no
		// automatic price: whether the plants fit together is a judgement
		// only the person packing them can make.
		box, measured := shippingBox(items)
		if input.CDEK.Repack || !measured {
			feePending = true
		} else if quotes, err := service.cdek.CalculatePVZ(ctx, input.CDEK.CityCode, box); err != nil {
			service.logger.Error("cdek quote failed at checkout", "error", err)
			feePending = true
		} else {
			// The cheapest tariff comes first, and that is what the checkout
			// preselects. A customer who chose a faster one is charged for it.
			quote := quotes[0]
			for _, option := range quotes {
				if option.TariffCode == input.CDEK.TariffCode {
					quote = option
					break
				}
			}
			deliveryFee = float64(quote.Price)
			tariffCode = &quote.TariffCode
		}

		// The address of the pick-up point is a convenience for the manager,
		// not a condition of the order. If CDEK will not tell us right now,
		// the code of the point is enough to look it up later.
		deliveryAddress = "Пункт выдачи СДЭК " + input.CDEK.OfficeCode
		if offices, err := service.cdek.GetOffices(ctx, input.CDEK.CityCode); err != nil {
			service.logger.Error("cdek offices failed at checkout", "error", err)
		} else {
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
			if address := selected.Location.AddressFull; address != "" {
				deliveryAddress = address
			} else if selected.Location.Address != "" {
				deliveryAddress = selected.Location.Address
			}
			if selected.Location.City != "" {
				resolvedCityName = selected.Location.City
			}
		}
		cityName = &resolvedCityName
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
			delivery_method, delivery_fee, delivery_fee_pending, delivery_repack_requested,
			cdek_city_code, cdek_city_name,
			cdek_office_code, cdek_tariff_code, subtotal, total, payment_status, status
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, $12, $13, $14, $15, $16, $17, 'payment_provider_pending', 'new'
		)
		RETURNING id
	`,
		orderNumber, customerID, input.Customer.Name, input.Customer.Phone,
		input.Customer.Email, deliveryAddress, input.Customer.Comment, input.Delivery,
		deliveryFee, boolToInt(feePending), boolToInt(input.CDEK.Repack && input.Delivery == "cdek"),
		cityCode, cityName, officeCode, tariffCode, subtotal, total,
	).Scan(&orderID)
	if err != nil {
		return Created{}, fmt.Errorf("insert order: %w", err)
	}
	for _, item := range items {
		if _, err := transaction.Exec(ctx, `
			INSERT INTO order_items (
				order_id, product_id, variant_id, product_name, unit_price, quantity
			) VALUES ($1, $2, $3, $4, $5, $6)
		`, orderID, item.ID, item.VariantID, item.Name, item.Price, item.Quantity); err != nil {
			return Created{}, fmt.Errorf("insert order item: %w", err)
		}
	}
	// The agreement is written in the same transaction as the order, so an
	// order can never exist without the record of the consent behind it.
	if err := consent.Record(ctx, transaction, consent.Event{
		CustomerID: input.CustomerID,
		OrderID:    &orderID,
		Event:      consent.EventOrder,
		Phone:      input.Customer.Phone,
		IPAddress:  input.ClientIP,
		UserAgent:  input.UserAgent,
	}); err != nil {
		return Created{}, err
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
	notificationCity := ""
	if cityName != nil {
		notificationCity = *cityName
	}
	if err := service.notifier.SendOrder(ctx, integration.TelegramOrder{
		OrderNumber: orderNumber, DeliveryMethod: input.Delivery,
		DeliveryCity: notificationCity, DeliveryFee: deliveryFee,
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

// reserveStock claims the requested quantity for one variant. The
// inventory rows are locked first, so two customers racing for the last
// plant queue up instead of both succeeding: the second one finds the
// stock already taken and gets a clear message.
func reserveStock(ctx context.Context, transaction pgx.Tx, item purchasableItem) error {
	rows, err := transaction.Query(ctx, `
		SELECT id, GREATEST(available_qty - reserved_qty, 0)
		FROM inventory
		WHERE variant_id = $1
		ORDER BY id
		FOR UPDATE
	`, item.VariantID)
	if err != nil {
		return fmt.Errorf("lock inventory: %w", err)
	}
	type slot struct {
		id   int64
		free int
	}
	slots := make([]slot, 0, 4)
	available := 0
	for rows.Next() {
		var current slot
		if err := rows.Scan(&current.id, &current.free); err != nil {
			rows.Close()
			return fmt.Errorf("scan inventory: %w", err)
		}
		available += current.free
		slots = append(slots, current)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read inventory: %w", err)
	}

	if available < item.Quantity {
		if available == 0 {
			return invalid(fmt.Sprintf("%s: товар закончился", item.Name))
		}
		return invalid(fmt.Sprintf("%s: доступно только %d шт.", item.Name, available))
	}

	remaining := item.Quantity
	for _, current := range slots {
		if remaining == 0 {
			break
		}
		take := min(current.free, remaining)
		if take == 0 {
			continue
		}
		if _, err := transaction.Exec(ctx, `
			UPDATE inventory SET reserved_qty = reserved_qty + $2 WHERE id = $1
		`, current.id, take); err != nil {
			return fmt.Errorf("reserve inventory: %w", err)
		}
		remaining -= take
	}
	return nil
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
