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
	OrderNumber   string  `json:"orderNumber"`
	PaymentStatus string  `json:"paymentStatus"`
	HasPreorder   bool    `json:"hasPreorder"`
	Total         float64 `json:"-"`
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
	ID            string // immutable SKU snapshot
	ProductID     int64
	VariantID     int64
	VariantLabel  string
	HeightCM      *int
	PotDiameterCM *int
	Name          string
	Price         float64
	Quantity      int
	Parcel        integration.Parcel
	// Preorder means the shelf could not cover this line. The order still
	// goes through; the manager names the date.
	Preorder bool
	// Checkout currently does not reserve any stock. This remains zero for new orders.
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

func (service *Service) Create(ctx context.Context, input CreateInput) (Created, error) {
	if !input.Consent {
		return Created{}, invalid("Подтвердите согласие на обработку персональных данных")
	}
	if input.Delivery != "pickup" && input.Delivery != "cdek" {
		return Created{}, invalid("Выберите доступный способ получения")
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
			SELECT pv.sku, p.id, p.name, pv.id, pv.label, variant_numeric_attribute(pv.id, 'height_cm')::INTEGER, variant_numeric_attribute(pv.id, 'pot_diameter_cm')::INTEGER, pv.base_price_minor,
				COALESCE(variant_numeric_attribute(pv.id, 'package_length_cm'), 0)::INTEGER, COALESCE(variant_numeric_attribute(pv.id, 'package_width_cm'), 0)::INTEGER,
				COALESCE(variant_numeric_attribute(pv.id, 'package_height_cm'), 0)::INTEGER, COALESCE(variant_numeric_attribute(pv.id, 'package_weight_grams'), 0)::INTEGER
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
	} else if !regularDelivery {
		return Created{}, invalid("Выберите способ получения")
	}

	// Availability is a checkout snapshot only. No order reserves stock; availability
	// must be checked again before payment or fulfilment.
	hasPreorder := false
	for index := range items {
		preorder, err := needsPreorder(ctx, transaction, items[index])
		if err != nil {
			return Created{}, err
		}
		items[index].Preorder = preorder
		hasPreorder = hasPreorder || preorder
	}
	// A missing plant makes the entire order a manager-confirmed request.
	// The customer need not choose a payment method for an order that cannot be paid yet.
	paymentMethod := strings.TrimSpace(input.PaymentMethod)
	if hasPreorder {
		paymentMethod = payment.MethodManager
	} else if !payment.Allowed(
		paymentMethod,
		input.Delivery,
		input.WholesaleApproved,
		input.OnlinePaymentReady,
	) {
		return Created{}, invalid("Выберите доступный способ оплаты")
	}

	orderNumber, err := newOrderNumber(ctx, transaction, input.CustomerID)
	if err != nil {
		return Created{}, err
	}
	// The shop collects the delivery charge together with the plants. If the
	// quote is pending, payment stays unavailable until a manager sets it.
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
			payment_method, payment_status, has_preorder, status, delivery_payee
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, 'new', 'shop'
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
		HasPreorder:   hasPreorder,
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

	return Created{OrderNumber: orderNumber, PaymentStatus: payment.InitialStatus(paymentMethod), HasPreorder: hasPreorder, Total: total}, nil
}

func needsPreorder(ctx context.Context, transaction pgx.Tx, item purchasableItem) (bool, error) {
	var available int
	err := transaction.QueryRow(ctx, `
		SELECT COALESCE(SUM(GREATEST(i.available_qty - i.reserved_qty, 0)), 0)::INTEGER
		FROM inventory i
		JOIN warehouses w ON w.id = i.warehouse_id AND w.saby_id = 'saby-ryazan-main' AND w.is_active = 1
		JOIN product_variants v ON v.id = i.variant_id
		JOIN products p ON p.id = v.product_id
		JOIN saby_nomenclature n ON n.saby_id = v.saby_id AND n.missing_since IS NULL
		WHERE i.variant_id = $1 AND 'stock' = ANY(p.saby_fields)
			AND i.synced_at >= CURRENT_TIMESTAMP - INTERVAL '2 hours'
	`, item.VariantID).Scan(&available)
	if err != nil {
		return false, fmt.Errorf("read inventory availability: %w", err)
	}
	return available < item.Quantity, nil
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
	if err := transaction.QueryRow(ctx, `
		SELECT COALESCE(MAX(split_part(order_number, '-', 2)::BIGINT), 0)
		FROM orders
		WHERE split_part(order_number, '-', 1) = $1
		  AND order_number ~ '^[0-9]+-[0-9]+$'
	`, prefix).Scan(&placed); err != nil {
		return "", fmt.Errorf("read last order number: %w", err)
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
