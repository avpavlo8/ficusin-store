package order

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/avpavlo8/ficusin-store/backend/internal/consent"
	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/avpavlo8/ficusin-store/backend/internal/mail"
	"github.com/avpavlo8/ficusin-store/backend/internal/payment"
	"github.com/avpavlo8/ficusin-store/backend/internal/settings"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type CDEK interface {
	GetOffices(context.Context, int) ([]integration.CDEKOffice, error)
	CalculatePVZ(context.Context, int, integration.Parcel) ([]integration.CDEKQuote, error)
}

type DeliveryPricer interface {
	Configured() bool
	Calculate(context.Context, string, integration.Parcel) (integration.DeliveryQuote, error)
}

type Notifier interface {
	SendOrder(context.Context, integration.TelegramOrder) error
}

type Service struct {
	pool     *pgxpool.Pool
	cdek     CDEK
	post     DeliveryPricer
	courier  DeliveryPricer
	notifier Notifier
	settings settingsReader
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
	// PaymentMethod is what the customer chose at the checkout. Whether
	// they were allowed to choose it is decided before we get here.
	PaymentMethod string
	// WholesaleApproved gates the invoice option. It comes from the
	// customer's own record, never from the browser.
	WholesaleApproved bool
	// OnlinePaymentReady is false when the shop has no YooKassa keys.
	OnlinePaymentReady bool
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
	ID        string // immutable SKU snapshot
	ProductID int64
	VariantID int64
	VariantLabel string
	HeightCM *int
	PotDiameterCM *int
	Name      string
	Price     float64
	Quantity  int
	Parcel    integration.Parcel
	// Preorder means the shelf could not cover this line. The order still
	// goes through; the manager names the date.
	Preorder bool
	// Reserved — сколько штук реально снято со склада. У предзаказа меньше
	// количества, и вернуть при отмене нужно именно столько.
	Reserved int
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
	shopSettings settingsReader,
	logger *slog.Logger,
) *Service {
	return &Service{
		pool:     pool,
		cdek:     cdek,
		notifier: notifier,
		settings: shopSettings,
		logger:   logger,
	}
}

// WithDeliveryPricers wires the two address-delivery providers without
// changing the old constructor used by focused unit tests. Production always
// supplies real clients; empty credentials leave the corresponding method
// unavailable instead of silently falling back to a made-up fixed price.
func (service *Service) WithDeliveryPricers(post, courier DeliveryPricer) *Service {
	service.post = post
	service.courier = courier
	return service
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

	// Скидка берётся из карточки покупателя, а не из браузера, и читается в
	// той же транзакции, что и цены: между «показали цену» и «записали
	// заказ» владелец мог её изменить, и заказ должен посчитаться по одному
	// набору чисел.
	discountBPS, err := retailDiscountBPS(ctx, transaction, input.CustomerID)
	if err != nil {
		return Created{}, err
	}

	requestedItems := mergeItems(input.Items)
	items := make([]purchasableItem, 0, len(requestedItems))
	for _, requested := range requestedItems {
		var item purchasableItem
		var priceMinor int64
		err := transaction.QueryRow(ctx, `
			SELECT pv.sku, p.id, p.name, pv.id, pv.label, pv.height_cm, pv.pot_diameter_cm, pv.base_price_minor,
				COALESCE(pv.package_length_cm, 0), COALESCE(pv.package_width_cm, 0),
				COALESCE(pv.package_height_cm, 0), COALESCE(pv.package_weight_grams, 0)
			FROM products p
			JOIN product_variants pv ON pv.product_id = p.id AND pv.is_active = 1 AND pv.archived_at IS NULL
			WHERE pv.sku = $1 AND p.status = 'published'
			LIMIT 1
		`, requested.ID).Scan(
			&item.ID, &item.ProductID, &item.Name, &item.VariantID, &item.VariantLabel,
			&item.HeightCM, &item.PotDiameterCM, &priceMinor,
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
		item.Price = float64(discountedMinor(priceMinor, discountBPS)) / 100
		items = append(items, item)
	}
	if len(items) == 0 {
		return Created{}, invalid("Корзина пуста")
	}

	subtotal := 0.0
	for _, item := range items {
		subtotal += item.Price * float64(item.Quantity)
	}
	deliveryFee := 0.0
	regularDelivery := input.Delivery == "pickup"
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
		} else if len(quotes) == 0 {
			service.logger.Error("cdek returned no tariffs at checkout")
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
	} else if input.Delivery == "post" || input.Delivery == "courier" {
		regularDelivery = true
		pricer := service.post
		providerName := "Почта России"
		if input.Delivery == "courier" {
			pricer = service.courier
			providerName = "Яндекс Доставка"
		}
		if pricer == nil || !pricer.Configured() {
			return Created{}, invalid(providerName + " временно недоступна. Выберите другой способ доставки")
		}
		box, measured := shippingBox(items)
		if !measured {
			feePending = true
		} else if quote, quoteErr := pricer.Calculate(ctx, deliveryAddress, box); errors.Is(quoteErr, integration.ErrRussianPostAddress) {
			return Created{}, invalid("Почта России не смогла определить адрес. Укажите его точнее")
		} else if errors.Is(quoteErr, integration.ErrYandexOutsideRyazan) {
			return Created{}, invalid("Курьер Яндекс Доставки доступен только по Рязани")
		} else if quoteErr != nil {
			service.logger.Error("delivery quote failed at checkout", "provider", providerName, "error", quoteErr)
			feePending = true
		} else if quote.Price <= 0 {
			service.logger.Error("delivery provider returned empty price", "provider", providerName)
			feePending = true
		} else {
			deliveryFee = quote.Price
		}
	} else if !regularDelivery {
		return Created{}, invalid("Выберите способ получения")
	}

	// The browser names a payment method; these rules decide whether it may
	// have it. Never silently replace an unavailable method with card payment.
	paymentMethod := strings.TrimSpace(input.PaymentMethod)
	if !payment.Allowed(
		paymentMethod,
		input.Delivery,
		input.WholesaleApproved,
		input.OnlinePaymentReady,
	) {
		return Created{}, invalid("Выберите доступный способ оплаты")
	}

	// Резерв — последним действием перед записью заказа, и намеренно после
	// разговора с перевозчиком. Он блокирует строки склада до конца транзакции,
	// и пока блокировка держится, никто другой не может купить то же растение.
	hasPreorder := false
	for index := range items {
		reserved, preorder, err := reserveStock(ctx, transaction, items[index])
		if err != nil {
			return Created{}, err
		}
		items[index].Reserved = reserved
		items[index].Preorder = preorder
		hasPreorder = hasPreorder || preorder
	}

	orderNumber, err := newOrderNumber(ctx, transaction, input.CustomerID)
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
			cdek_office_code, cdek_tariff_code, subtotal, total,
			payment_method, payment_status, has_preorder, status
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, 'new'
		)
		RETURNING id
	`,
		orderNumber, customerID, input.Customer.Name, input.Customer.Phone,
		input.Customer.Email, deliveryAddress, input.Customer.Comment, input.Delivery,
		deliveryFee, boolToInt(feePending), boolToInt(input.CDEK.Repack && input.Delivery == "cdek"),
		cityCode, cityName, officeCode, tariffCode, subtotal, total,
		paymentMethod, payment.InitialStatus(paymentMethod), boolToInt(hasPreorder),
	).Scan(&orderID)
	if err != nil {
		return Created{}, fmt.Errorf("insert order: %w", err)
	}
	for _, item := range items {
		if _, err := transaction.Exec(ctx, `
			INSERT INTO order_items (
				order_id, product_id, variant_id, sku, product_name, variant_label, variant_snapshot,
				unit_price, quantity, is_preorder, reserved_qty
			) VALUES ($1, $2, $3, $4, $5, $6,
				jsonb_strip_nulls(jsonb_build_object('heightCm',$7::INTEGER,'potDiameterCm',$8::INTEGER)),
				$9, $10, $11, $12)
		`, orderID, item.ProductID, item.VariantID, item.ID, item.Name, item.VariantLabel,
			item.HeightCM, item.PotDiameterCM, item.Price, item.Quantity,
			boolToInt(item.Preorder), item.Reserved); err != nil {
			return Created{}, fmt.Errorf("insert order item: %w", err)
		}
	}
	if err := RecordMovement(ctx, transaction, orderID, MovementReserve); err != nil {
		return Created{}, err
	}
	if err := recordPreorderRequests(ctx, transaction, orderID); err != nil {
		return Created{}, err
	}
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
	letter := mail.Confirmation(mail.OrderLetter{
		Number:        orderNumber,
		CustomerName:  input.Customer.Name,
		Items:         letterLines(items),
		Subtotal:      subtotal,
		DeliveryFee:   deliveryFee,
		FeePending:    feePending,
		Total:         total,
		Delivery:      input.Delivery,
		Address:       deliveryAddress,
		PaymentStatus: payment.InitialStatus(paymentMethod),
		PaymentMethod: paymentMethod,
	})
	if _, err := transaction.Exec(ctx, `
		INSERT INTO outbox (recipient, subject, body) VALUES ($1, $2, $3)
	`, input.Customer.Email, letter.Subject, letter.Body); err != nil {
		return Created{}, fmt.Errorf("queue confirmation letter: %w", err)
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

	return Created{OrderNumber: orderNumber, PaymentStatus: payment.InitialStatus(paymentMethod)}, nil
}

// deliveryFee is retained for old settings tests and existing installations.
// New customer-facing courier and post prices are authoritative provider
// quotes; these fixed numbers are no longer used to create new orders.
func (service *Service) deliveryFee(key string) float64 {
	value := settings.DefaultNumber(key)
	if service.settings != nil {
		value = service.settings.Number(key)
	}
	return float64(settings.NonNegative(value))
}

func reserveStock(ctx context.Context, transaction pgx.Tx, item purchasableItem) (int, bool, error) {
	rows, err := transaction.Query(ctx, `
		SELECT id, GREATEST(available_qty - reserved_qty, 0)
		FROM inventory
		WHERE variant_id = $1
		ORDER BY id
		FOR UPDATE
	`, item.VariantID)
	if err != nil {
		return 0, false, fmt.Errorf("lock inventory: %w", err)
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
			return 0, false, fmt.Errorf("scan inventory: %w", err)
		}
		available += current.free
		slots = append(slots, current)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, false, fmt.Errorf("read inventory: %w", err)
	}
	if available == 0 {
		return 0, true, nil
	}
	preorder := available < item.Quantity
	remaining := min(item.Quantity, available)
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
			return 0, false, fmt.Errorf("reserve inventory: %w", err)
		}
		remaining -= take
	}
	return min(item.Quantity, available), preorder, nil
}

func retailDiscountBPS(ctx context.Context, transaction pgx.Tx, customerID *int64) (int, error) {
	if customerID == nil {
		return 0, nil
	}
	var bps int
	if err := transaction.QueryRow(ctx, `
		SELECT COALESCE(retail_discount_bps, 0) FROM customers WHERE id = $1
	`, *customerID).Scan(&bps); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("load customer discount: %w", err)
	}
	return bps, nil
}

func discountedMinor(priceMinor int64, bps int) int64 {
	if bps <= 0 {
		return priceMinor
	}
	if bps > 9000 {
		bps = 9000
	}
	return (priceMinor*int64(10000-bps) + 5000) / 10000
}

func newOrderNumber(ctx context.Context, transaction pgx.Tx, customerID *int64) (string, error) {
	prefix := "0000"
	if customerID != nil {
		prefix = fmt.Sprintf("%04d", *customerID)
	}
	if _, err := transaction.Exec(ctx, `
		SELECT pg_advisory_xact_lock(hashtext('ficusin-order-number:' || $1))
	`, prefix); err != nil {
		return "", fmt.Errorf("lock order number: %w", err)
	}

	var placed int
	if customerID != nil {
		if err := transaction.QueryRow(ctx, `
			SELECT COUNT(*)::INTEGER FROM orders WHERE customer_id = $1
		`, *customerID).Scan(&placed); err != nil {
			return "", fmt.Errorf("count customer orders: %w", err)
		}
	} else if err := transaction.QueryRow(ctx, `
		SELECT COUNT(*)::INTEGER FROM orders WHERE customer_id IS NULL
	`).Scan(&placed); err != nil {
		return "", fmt.Errorf("count guest orders: %w", err)
	}
	return formatOrderNumber(prefix, placed+1), nil
}

func formatOrderNumber(prefix string, sequence int) string {
	return fmt.Sprintf("%s-%d", prefix, sequence)
}

func letterLines(items []purchasableItem) []mail.OrderLine {
	lines := make([]mail.OrderLine, 0, len(items))
	for _, item := range items {
		lines = append(lines, mail.OrderLine{
			Name: item.Name, Price: item.Price, Quantity: item.Quantity,
		})
	}
	return lines
}
