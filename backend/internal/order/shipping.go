package order

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/avpavlo8/ficusin-store/backend/internal/settings"
	"github.com/jackc/pgx/v5/pgxpool"
)

// shipper is the slice of CDEK this worker needs.
type shipper interface {
	Configured() bool
	CreateOrder(context.Context, integration.ShipmentRequest) (integration.Shipment, error)
	FetchOrder(ctx context.Context, uuid string) (integration.Shipment, error)
}

type shippingSettings interface {
	Enabled(key string) bool
	Value(key string) string
}

// cdekStatusToOrder maps what CDEK says about a parcel onto what the shop
// tells the customer. Only the milestones people care about are mapped;
// CDEK has dozens of internal states, and echoing all of them would turn
// the order page into a logistics log.
var cdekStatusToOrder = map[string]string{
	"RECEIVED_AT_SHIPMENT_WAREHOUSE": "shipped",
	"IN_TRANSIT":                     "shipped",
	"ACCEPTED_AT_TRANSIT_WAREHOUSE":  "shipped",
	"RECEIVED_AT_DELIVERY_WAREHOUSE": "ready",
	"READY_FOR_RECIPIENT":            "ready",
	"DELIVERED":                      "completed",
	"NOT_DELIVERED":                  "cancelled",
}

// ShippingWorker hands paid orders to CDEK and keeps their status current.
//
// It is switched off by default and turned on in the panel, so that test
// orders during a quiet afternoon do not create real parcels.
type ShippingWorker struct {
	pool     *pgxpool.Pool
	cdek     shipper
	settings shippingSettings
	notifier statusNotifier
	logger   *slog.Logger
	interval time.Duration
}

type statusNotifier interface {
	NotifyOrderStatus(ctx context.Context, customerID int64, orderNumber, status string) error
}

func NewShippingWorker(
	pool *pgxpool.Pool,
	cdek shipper,
	shopSettings shippingSettings,
	notifier statusNotifier,
	logger *slog.Logger,
) *ShippingWorker {
	return &ShippingWorker{
		pool:     pool,
		cdek:     cdek,
		settings: shopSettings,
		notifier: notifier,
		logger:   logger,
		interval: 2 * time.Minute,
	}
}

func (worker *ShippingWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(worker.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !worker.settings.Enabled(settings.CDEKOrdersEnabled) || !worker.cdek.Configured() {
				continue
			}
			worker.createShipments(ctx)
			worker.refreshStatuses(ctx)
		}
	}
}

// createShipments registers parcels for orders that are ready to travel.
//
// Only paid orders, or ones that will be paid at the counter: handing an
// unpaid parcel to a carrier is giving away a plant and hoping.
func (worker *ShippingWorker) createShipments(ctx context.Context) {
	rows, err := worker.pool.Query(ctx, `
		SELECT o.id, o.order_number, o.customer_name, o.phone,
			COALESCE(o.cdek_office_code, ''), COALESCE(o.cdek_tariff_code, 0),
			COALESCE(o.cdek_city_code, 0), o.payment_status,
			o.total::DOUBLE PRECISION, o.delivery_fee::DOUBLE PRECISION
		FROM orders o
		WHERE o.delivery_method = 'cdek'
			AND o.cdek_uuid = ''
			AND o.delivery_fee_pending = 0
			AND o.status NOT IN ('cancelled', 'completed')
			AND o.payment_status IN ('paid', 'on_delivery')
		ORDER BY o.id
		LIMIT 20
	`)
	if err != nil {
		worker.logger.Error("find orders to ship failed", "error", err)
		return
	}
	type pending struct {
		id                                     int64
		number, name, phone, office, payStatus string
		tariff, city                           int
		total, deliveryFee                     float64
	}
	waiting := make([]pending, 0)
	for rows.Next() {
		var item pending
		if err := rows.Scan(
			&item.id, &item.number, &item.name, &item.phone, &item.office,
			&item.tariff, &item.city, &item.payStatus, &item.total, &item.deliveryFee,
		); err != nil {
			worker.logger.Error("scan order to ship failed", "error", err)
			break
		}
		waiting = append(waiting, item)
	}
	rows.Close()

	for _, item := range waiting {
		items, box, err := worker.shipmentContents(ctx, item.id)
		if err != nil {
			worker.logger.Error("load shipment contents failed", "error", err, "order_id", item.id)
			continue
		}
		cash := 0.0
		if item.payStatus == "on_delivery" {
			cash = item.total
		}
		shipment, err := worker.cdek.CreateOrder(ctx, integration.ShipmentRequest{
			OrderNumber:       item.number,
			TariffCode:        item.tariff,
			OfficeCode:        item.office,
			CityCode:          item.city,
			Box:               box,
			Items:             items,
			SenderName:        worker.settings.Value(settings.CDEKSenderName),
			SenderPhone:       worker.settings.Value(settings.CDEKSenderPhone),
			SenderAddress:     worker.settings.Value(settings.CDEKSenderAddress),
			RecipientName:     item.name,
			RecipientPhone:    item.phone,
			PaymentOnDelivery: cash,
		})
		if err != nil {
			// A refusal is usually about the data — a missing sender, a
			// pick-up point that closed. Logged for a person to look at;
			// the order itself is untouched and can be sent by hand.
			worker.logger.Error("create cdek order failed", "error", err, "order", item.number)
			continue
		}
		if _, err := worker.pool.Exec(ctx, `
			UPDATE orders SET cdek_uuid = $2, cdek_synced_at = CURRENT_TIMESTAMP
			WHERE id = $1
		`, item.id, shipment.UUID); err != nil {
			worker.logger.Error("store cdek uuid failed", "error", err, "order", item.number)
		}
	}
}

// refreshStatuses picks up tracking numbers and moves the order along as the
// parcel travels. CDEK registers a shipment asynchronously, so the tracking
// number is usually empty on the first ask and appears a minute later.
func (worker *ShippingWorker) refreshStatuses(ctx context.Context) {
	rows, err := worker.pool.Query(ctx, `
		SELECT id, order_number, customer_id, cdek_uuid, cdek_track_number,
			cdek_status, status
		FROM orders
		WHERE cdek_uuid <> ''
			AND status NOT IN ('cancelled', 'completed')
		ORDER BY id
		LIMIT 50
	`)
	if err != nil {
		worker.logger.Error("find shipments to refresh failed", "error", err)
		return
	}
	type tracked struct {
		id         int64
		number     string
		customerID *int64
		uuid       string
		track      string
		cdekStatus string
		status     string
	}
	shipments := make([]tracked, 0)
	for rows.Next() {
		var item tracked
		if err := rows.Scan(
			&item.id, &item.number, &item.customerID, &item.uuid,
			&item.track, &item.cdekStatus, &item.status,
		); err != nil {
			worker.logger.Error("scan shipment failed", "error", err)
			break
		}
		shipments = append(shipments, item)
	}
	rows.Close()

	for _, item := range shipments {
		shipment, err := worker.cdek.FetchOrder(ctx, item.uuid)
		if err != nil {
			worker.logger.Error("fetch cdek order failed", "error", err, "order", item.number)
			continue
		}
		if shipment.TrackNumber == item.track && shipment.Status == item.cdekStatus {
			continue
		}
		nextStatus := item.status
		if mapped, known := cdekStatusToOrder[strings.ToUpper(shipment.Status)]; known {
			nextStatus = mapped
		}
		if _, err := worker.pool.Exec(ctx, `
			UPDATE orders
			SET cdek_track_number = $2, cdek_status = $3, status = $4,
				cdek_synced_at = CURRENT_TIMESTAMP
			WHERE id = $1
		`, item.id, shipment.TrackNumber, shipment.Status, nextStatus); err != nil {
			worker.logger.Error("store shipment status failed", "error", err, "order", item.number)
			continue
		}
		// The customer hears about the milestone, not about every scan at
		// every warehouse along the way.
		if nextStatus != item.status && item.customerID != nil && worker.notifier != nil {
			_ = worker.notifier.NotifyOrderStatus(ctx, *item.customerID, item.number, nextStatus)
		}
	}
}

func (worker *ShippingWorker) shipmentContents(
	ctx context.Context,
	orderID int64,
) ([]integration.ShipmentItem, integration.Parcel, error) {
	rows, err := worker.pool.Query(ctx, `
		SELECT oi.product_name, oi.unit_price::DOUBLE PRECISION, oi.quantity,
			COALESCE(pv.package_length_cm, 0), COALESCE(pv.package_width_cm, 0),
			COALESCE(pv.package_height_cm, 0), COALESCE(pv.package_weight_grams, 0)
		FROM order_items oi
		LEFT JOIN product_variants pv ON pv.id = oi.variant_id
		WHERE oi.order_id = $1
		ORDER BY oi.id
	`, orderID)
	if err != nil {
		return nil, integration.Parcel{}, err
	}
	defer rows.Close()
	items := make([]integration.ShipmentItem, 0)
	parcels := make([]integration.Parcel, 0)
	for rows.Next() {
		var item integration.ShipmentItem
		var parcel integration.Parcel
		if err := rows.Scan(
			&item.Name, &item.Price, &item.Quantity,
			&parcel.LengthCM, &parcel.WidthCM, &parcel.HeightCM, &parcel.WeightGrams,
		); err != nil {
			return nil, integration.Parcel{}, err
		}
		item.WeightGrams = parcel.WeightGrams
		items = append(items, item)
		for count := 0; count < max(1, item.Quantity); count++ {
			parcels = append(parcels, parcel)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, integration.Parcel{}, err
	}
	box, _ := integration.CombineParcels(parcels)
	return items, box, nil
}
