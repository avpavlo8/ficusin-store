package order

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	CancelOrder(ctx context.Context, uuid string) error
	FindOrderByNumber(ctx context.Context, number string) (integration.Shipment, error)
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
	"READY_FOR_SHIPMENT_IN_SENDER_CITY": "shipped",
	"TAKEN_BY_TRANSPORTER_FROM_SENDER_CITY": "shipped",
	"SENT_TO_RECIPIENT_CITY":          "shipped",
	"IN_TRANSIT":                     "shipped",
	"ACCEPTED_IN_TRANSIT_CITY":        "shipped",
	"ACCEPTED_AT_TRANSIT_WAREHOUSE":  "shipped",
	"RECEIVED_AT_DELIVERY_WAREHOUSE": "ready",
	"ACCEPTED_AT_PICK_UP_POINT":       "ready",
	"POSTOMAT_POSTED":                 "ready",
	"READY_FOR_RECIPIENT":            "ready",
	"DELIVERED":                      "completed",
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
	worker.sync(ctx)
	ticker := time.NewTicker(worker.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			worker.sync(ctx)
		}
	}
}

func (worker *ShippingWorker) sync(ctx context.Context) {
	if worker.cdek == nil || !worker.cdek.Configured() {
		return
	}
	// The switch controls creation of real shipments only. Existing parcels
	// must keep being tracked even if a manager pauses automatic hand-off.
	worker.reconcileUnknownShipments(ctx)
	if worker.settings.Enabled(settings.CDEKOrdersEnabled) {
		worker.createOfferShipments(ctx)
		worker.createShipments(ctx)
	}
	worker.cancelShipments(ctx)
	worker.refreshStatuses(ctx)
	worker.refreshOfferStatuses(ctx)
}

// createOfferShipments sends the immutable offer packing to CDEK. The parent
// order can still contain plants expected later, so it is deliberately left
// open and keeps its own delivery state.
func (worker *ShippingWorker) createOfferShipments(ctx context.Context) {
	rows, err := worker.pool.Query(ctx, `
		WITH candidates AS (
			SELECT so.id FROM shipment_offers so JOIN orders o ON o.id=so.order_id
				WHERE so.status='paid' AND so.delivery_method='cdek' AND so.cdek_uuid=''
					AND so.cdek_create_state NOT IN ('unknown','manual_review')
				AND (so.cdek_next_attempt_at IS NULL OR so.cdek_next_attempt_at<=CURRENT_TIMESTAMP)
				AND o.status NOT IN ('cancelled','completed')
			ORDER BY so.cdek_next_attempt_at ASC NULLS FIRST,so.id FOR UPDATE OF so SKIP LOCKED LIMIT 20
		), claimed AS (
			UPDATE shipment_offers so SET status='shipping',cdek_create_state='creating',
				cdek_attempts=cdek_attempts+1,cdek_next_attempt_at=CURRENT_TIMESTAMP+INTERVAL '15 minutes'
			FROM candidates c WHERE so.id=c.id RETURNING so.*
		)
		SELECT so.id,o.id,o.order_number,o.customer_name,o.phone,COALESCE(o.cdek_office_code,''),
			COALESCE(so.cdek_tariff_code,0),COALESCE(o.cdek_city_code,0),so.cdek_attempts
		FROM claimed so JOIN orders o ON o.id=so.order_id ORDER BY so.id`)
	if err != nil {
		worker.logger.Error("find shipment offers to ship failed", "error", err)
		return
	}
	type pending struct {
		offerID, orderID          int64
		number, name, phone, office string
		tariff, city, attempts     int
	}
	waiting := make([]pending, 0)
	for rows.Next() {
		var item pending
		if err := rows.Scan(&item.offerID, &item.orderID, &item.number, &item.name, &item.phone, &item.office, &item.tariff, &item.city, &item.attempts); err != nil {
			worker.logger.Error("scan shipment offer failed", "error", err)
			break
		}
		waiting = append(waiting, item)
	}
	rows.Close()
	for _, item := range waiting {
		packages, err := worker.offerPackages(ctx, item.offerID, item.number)
		if err == nil && len(packages) == 0 {
			err = errors.New("у предложения нет коробок")
		}
		if err != nil {
			worker.recordOfferCreateFailure(ctx, item.offerID, item.number, item.attempts, err)
			continue
		}
		shipment, err := worker.cdek.CreateOrder(ctx, integration.ShipmentRequest{
			OrderNumber:     fmt.Sprintf("%s-%d", item.number, item.offerID),
			TariffCode:      item.tariff,
			OfficeCode:      item.office,
			CityCode:        item.city,
			Packages:        packages,
			SenderName:      worker.settings.Value(settings.CDEKSenderName),
			SenderPhone:     worker.settings.Value(settings.CDEKSenderPhone),
			SenderAddress:   worker.settings.Value(settings.CDEKSenderAddress),
			RecipientName:   item.name,
			RecipientPhone:  item.phone,
		})
		if err != nil {
			worker.recordOfferCreateFailure(ctx, item.offerID, item.number, item.attempts, err)
			continue
		}
		if _, err := worker.pool.Exec(ctx, `
			UPDATE shipment_offers
			SET cdek_uuid=$2,cdek_track_number=$3,cdek_status=$4,cdek_status_reason=$5,
				cdek_create_state='registered',cdek_last_error='',cdek_next_attempt_at=NULL,
				cdek_synced_at=CURRENT_TIMESTAMP,status='shipping',updated_at=CURRENT_TIMESTAMP
			WHERE id=$1 AND cdek_uuid=''`, item.offerID, shipment.UUID, shipment.TrackNumber, shipment.Status, shipment.StatusReason); err != nil {
			worker.logger.Error("store offer cdek uuid failed", "error", err, "order", item.number)
		}
	}
}

func (worker *ShippingWorker) offerPackages(ctx context.Context,offerID int64,number string)([]integration.ShipmentPackage,error){
	itemRows,err:=worker.pool.Query(ctx,`SELECT order_item_id,product_name,unit_price::DOUBLE PRECISION,quantity,COALESCE(variant_numeric_attribute(variant_id,'package_weight_grams'),0)::INTEGER FROM shipment_offer_items WHERE shipment_offer_id=$1 ORDER BY id`,offerID);if err!=nil{return nil,err}
	items:=map[int64]integration.ShipmentItem{};for itemRows.Next(){var id int64;var item integration.ShipmentItem;if err:=itemRows.Scan(&id,&item.Name,&item.Price,&item.Quantity,&item.WeightGrams);err!=nil{itemRows.Close();return nil,err};items[id]=item};itemRows.Close();if err:=itemRows.Err();err!=nil{return nil,err}
	boxRows,err:=worker.pool.Query(ctx,`SELECT box_no,length_cm,width_cm,height_cm,weight_grams,contents FROM shipment_offer_boxes WHERE shipment_offer_id=$1 ORDER BY box_no`,offerID);if err!=nil{return nil,err};defer boxRows.Close()
	result:=[]integration.ShipmentPackage{};used:=map[int64]bool{}
		type boxContent struct{OrderItemID int64 `json:"orderItemId"`;Quantity int `json:"quantity"`}
		packed:=map[int64]int{}
		for boxRows.Next(){var boxNo int;var box integration.Parcel;var raw []byte;if err:=boxRows.Scan(&boxNo,&box.LengthCM,&box.WidthCM,&box.HeightCM,&box.WeightGrams,&raw);err!=nil{return nil,err};var assigned []boxContent;if err:=json.Unmarshal(raw,&assigned);err!=nil{return nil,err};contents:=[]integration.ShipmentItem{};for _,content:=range assigned{item,ok:=items[content.OrderItemID];if !ok||content.Quantity<=0{return nil,fmt.Errorf("некорректный состав коробки %d",boxNo)};item.Quantity=content.Quantity;packed[content.OrderItemID]+=content.Quantity;used[content.OrderItemID]=true;contents=append(contents,item)};if len(contents)==0{return nil,fmt.Errorf("коробка %d пуста",boxNo)};result=append(result,integration.ShipmentPackage{Number:fmt.Sprintf("%s-%d",number,boxNo),Box:box,Items:contents})}
	if err:=boxRows.Err();err!=nil{return nil,err};if len(used)!=len(items){return nil,errors.New("не все позиции распределены по коробкам")};for id,item:=range items{if packed[id]!=item.Quantity{return nil,errors.New("количество товара в коробках не совпадает с предложением")}};return result,nil
}

func (worker *ShippingWorker) recordOfferCreateFailure(ctx context.Context, offerID int64, number string, attempts int, failure error) {
	delay := time.Minute * time.Duration(1<<min(6, max(0, attempts-1)))
	state, status := "retry", "paid"
	if errors.Is(failure, integration.ErrCDEKOutcomeUnknown) {
		state, status = "unknown", "shipping"
		delay = 2 * time.Minute
	}
	message := failure.Error()
	if len(message) > 1000 {
		message = message[:1000]
	}
	if _, err := worker.pool.Exec(ctx, `
		UPDATE shipment_offers
		SET status=$2,cdek_create_state=$3,cdek_last_error=$4,
			cdek_next_attempt_at=CURRENT_TIMESTAMP+($5*INTERVAL '1 second'),updated_at=CURRENT_TIMESTAMP
		WHERE id=$1 AND cdek_uuid=''`, offerID, status, state, message, int(delay/time.Second)); err != nil {
		worker.logger.Error("store offer cdek failure failed", "error", err, "order", number)
	}
}

// reconcileUnknownShipments uses a read-only lookup by our stable order
// number. It never repeats POST after a lost create response.
func (worker *ShippingWorker) reconcileUnknownShipments(ctx context.Context) {
	rows, err := worker.pool.Query(ctx, `
		UPDATE orders
		SET cdek_attempts=cdek_attempts+1,cdek_next_attempt_at=CURRENT_TIMESTAMP+INTERVAL '5 minutes'
		WHERE id IN (
			SELECT id FROM orders
			WHERE cdek_create_state='unknown' AND cdek_uuid=''
				AND COALESCE(cdek_next_attempt_at,CURRENT_TIMESTAMP)<=CURRENT_TIMESTAMP
			ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 20
		)
		RETURNING id,order_number,status,cdek_attempts`)
	if err != nil {
		worker.logger.Error("claim unknown cdek orders failed", "error", err)
		return
	}
	type pendingOrder struct {
		id               int64
		number, status   string
		attempts         int
	}
	orders := []pendingOrder{}
	for rows.Next() {
		var item pendingOrder
		if err := rows.Scan(&item.id, &item.number, &item.status, &item.attempts); err != nil {
			rows.Close()
			return
		}
		orders = append(orders, item)
	}
	rows.Close()
	for _, item := range orders {
		shipment, findErr := worker.cdek.FindOrderByNumber(ctx, item.number)
		if findErr == nil && shipment.UUID != "" {
			nextStatus := item.status
			if mapped, ok := cdekStatusToOrder[strings.ToUpper(shipment.Status)]; ok {
				nextStatus = mapped
			}
			_, err = worker.pool.Exec(ctx, `
				UPDATE orders
				SET cdek_uuid=$2,cdek_track_number=$3,cdek_status=$4,cdek_status_reason=$5,
					cdek_create_state='registered',cdek_last_error='',cdek_next_attempt_at=NULL,
					cdek_synced_at=CURRENT_TIMESTAMP,status=$6
				WHERE id=$1 AND cdek_uuid=''`, item.id, shipment.UUID, shipment.TrackNumber, shipment.Status, shipment.StatusReason, nextStatus)
			if err != nil {
				worker.logger.Error("store reconciled cdek order failed", "error", err, "order", item.number)
			}
			continue
		}
		if findErr == nil {
			findErr = integration.ErrCDEKOrderNotFound
		}
		state, nextAttempt := cdekReconciliationFailure(findErr, item.attempts)
		_, _ = worker.pool.Exec(ctx, `
			UPDATE orders SET cdek_create_state=$2,cdek_last_error=$3,cdek_next_attempt_at=$4
			WHERE id=$1 AND cdek_uuid=''`, item.id, state, findErr.Error(), nextAttempt)
	}

	offerRows, err := worker.pool.Query(ctx, `
		UPDATE shipment_offers
		SET cdek_attempts=cdek_attempts+1,cdek_next_attempt_at=CURRENT_TIMESTAMP+INTERVAL '5 minutes'
		WHERE id IN (
			SELECT id FROM shipment_offers
			WHERE cdek_create_state='unknown' AND cdek_uuid=''
				AND COALESCE(cdek_next_attempt_at,CURRENT_TIMESTAMP)<=CURRENT_TIMESTAMP
			ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 20
		)
		RETURNING id,order_id,status,cdek_attempts`)
	if err != nil {
		worker.logger.Error("claim unknown cdek offers failed", "error", err)
		return
	}
	type pendingOffer struct {
		id, orderID int64
		status      string
		attempts    int
	}
	offers := []pendingOffer{}
	for offerRows.Next() {
		var item pendingOffer
		if err := offerRows.Scan(&item.id, &item.orderID, &item.status, &item.attempts); err != nil {
			offerRows.Close()
			return
		}
		offers = append(offers, item)
	}
	offerRows.Close()
	for _, item := range offers {
		var number string
		if err := worker.pool.QueryRow(ctx, `SELECT order_number FROM orders WHERE id=$1`, item.orderID).Scan(&number); err != nil {
			continue
		}
		providerNumber := fmt.Sprintf("%s-%d", number, item.id)
		shipment, findErr := worker.cdek.FindOrderByNumber(ctx, providerNumber)
		if findErr == nil && shipment.UUID != "" {
			nextStatus := "shipping"
			if mapped, ok := cdekStatusToOrder[strings.ToUpper(shipment.Status)]; ok {
				nextStatus = mapped
			}
			_, err = worker.pool.Exec(ctx, `
				UPDATE shipment_offers
				SET cdek_uuid=$2,cdek_track_number=$3,cdek_status=$4,cdek_status_reason=$5,
					cdek_create_state='registered',cdek_last_error='',cdek_next_attempt_at=NULL,
					cdek_synced_at=CURRENT_TIMESTAMP,status=$6,updated_at=CURRENT_TIMESTAMP
				WHERE id=$1 AND cdek_uuid=''`, item.id, shipment.UUID, shipment.TrackNumber, shipment.Status, shipment.StatusReason, nextStatus)
			if err != nil {
				worker.logger.Error("store reconciled cdek offer failed", "error", err, "offer_id", item.id)
			}
			continue
		}
		if findErr == nil {
			findErr = integration.ErrCDEKOrderNotFound
		}
		state, nextAttempt := cdekReconciliationFailure(findErr, item.attempts)
		_, _ = worker.pool.Exec(ctx, `
			UPDATE shipment_offers
			SET cdek_create_state=$2,cdek_last_error=$3,cdek_next_attempt_at=$4,updated_at=CURRENT_TIMESTAMP
			WHERE id=$1 AND cdek_uuid=''`, item.id, state, findErr.Error(), nextAttempt)
	}
}

func cdekReconciliationFailure(failure error, attempts int) (string, any) {
	if attempts >= 4 && !errors.Is(failure, integration.ErrCDEKOutcomeUnknown) {
		return "manual_review", nil
	}
	return "unknown", time.Now().Add(5 * time.Minute)
}

func (worker *ShippingWorker) cancelShipments(ctx context.Context) {
	rows, err := worker.pool.Query(ctx, `
		WITH candidates AS (
			SELECT id FROM orders
			WHERE status = 'cancelled' AND cdek_uuid <> ''
				AND cdek_cancel_state NOT IN ('cancelled', 'manual_review')
				AND (cdek_cancel_next_attempt_at IS NULL OR cdek_cancel_next_attempt_at <= CURRENT_TIMESTAMP)
			ORDER BY cdek_cancel_next_attempt_at ASC NULLS FIRST, id
			FOR UPDATE SKIP LOCKED LIMIT 20
		), claimed AS (
			UPDATE orders o SET cdek_cancel_state = 'cancelling',
				cdek_cancel_attempts = cdek_cancel_attempts + 1,
				cdek_cancel_next_attempt_at = CURRENT_TIMESTAMP + INTERVAL '15 minutes'
			FROM candidates c WHERE o.id = c.id
			RETURNING o.id, o.order_number, o.cdek_uuid, o.cdek_cancel_attempts
		)
		SELECT id, order_number, cdek_uuid, cdek_cancel_attempts FROM claimed
	`)
	if err != nil {
		worker.logger.Error("find cdek cancellations failed", "error", err)
		return
	}
	type cancellation struct { id int64; number, uuid string; attempts int }
	items := make([]cancellation, 0)
	for rows.Next() {
		var item cancellation
		if err := rows.Scan(&item.id, &item.number, &item.uuid, &item.attempts); err != nil {
			worker.logger.Error("scan cdek cancellation failed", "error", err)
			break
		}
		items = append(items, item)
	}
	rows.Close()
	for _, item := range items {
		if err := worker.cdek.CancelOrder(ctx, item.uuid); err != nil {
			state := "retry"
			if item.attempts >= 8 { state = "manual_review" }
			delay := time.Minute * time.Duration(1<<min(6, max(0, item.attempts-1)))
			message := err.Error()
			if len(message) > 1000 { message = message[:1000] }
			_, saveErr := worker.pool.Exec(ctx, `
				UPDATE orders SET cdek_cancel_state = $2, cdek_last_error = $3,
					cdek_cancel_next_attempt_at = CURRENT_TIMESTAMP + ($4 * INTERVAL '1 second')
				WHERE id = $1
			`, item.id, state, message, int(delay/time.Second))
			if saveErr != nil { worker.logger.Error("store cdek cancellation failure failed", "error", saveErr, "order", item.number) }
			continue
		}
		if _, err := worker.pool.Exec(ctx, `
			UPDATE orders SET cdek_cancel_state = 'cancelled', cdek_last_error = '',
				cdek_cancel_next_attempt_at = NULL, cdek_synced_at = CURRENT_TIMESTAMP
			WHERE id = $1
		`, item.id); err != nil {
			worker.logger.Error("store cdek cancellation failed", "error", err, "order", item.number)
		}
	}
}

// createShipments registers parcels for orders that are ready to travel.
//
// Only paid orders, or ones that will be paid at the counter: handing an
// unpaid parcel to a carrier is giving away a plant and hoping.
func (worker *ShippingWorker) createShipments(ctx context.Context) {
	rows, err := worker.pool.Query(ctx, `
		WITH candidates AS (
			SELECT o.id
			FROM orders o
				WHERE o.delivery_method = 'cdek'
					AND o.cdek_uuid = ''
					AND o.cdek_create_state NOT IN ('unknown','manual_review')
				AND o.delivery_fee_pending = 0
				AND o.status NOT IN ('cancelled', 'completed')
				AND o.payment_status IN ('paid', 'on_delivery')
				AND NOT EXISTS (SELECT 1 FROM shipment_offers so WHERE so.order_id=o.id AND so.status IN ('paid','shipping','shipped','ready','completed'))
				AND (o.cdek_next_attempt_at IS NULL OR o.cdek_next_attempt_at <= CURRENT_TIMESTAMP)
			ORDER BY o.cdek_next_attempt_at ASC NULLS FIRST, o.id
			FOR UPDATE SKIP LOCKED
			LIMIT 20
		), claimed AS (
			UPDATE orders o
			SET cdek_create_state = 'creating', cdek_attempts = cdek_attempts + 1,
				cdek_next_attempt_at = CURRENT_TIMESTAMP + INTERVAL '15 minutes'
			FROM candidates c
			WHERE o.id = c.id
			RETURNING o.*
		)
		SELECT o.id, o.order_number, o.customer_name, o.phone,
			COALESCE(o.cdek_office_code, ''), COALESCE(o.cdek_tariff_code, 0),
			COALESCE(o.cdek_city_code, 0), o.payment_status,
			o.total::DOUBLE PRECISION, o.delivery_fee::DOUBLE PRECISION,
			o.cdek_attempts
		FROM claimed o
		ORDER BY o.id
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
		attempts                               int
	}
	waiting := make([]pending, 0)
	for rows.Next() {
		var item pending
		if err := rows.Scan(
			&item.id, &item.number, &item.name, &item.phone, &item.office,
			&item.tariff, &item.city, &item.payStatus, &item.total, &item.deliveryFee,
			&item.attempts,
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
			worker.recordCreateFailure(ctx, item.id, item.number, item.attempts, err)
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
			worker.recordCreateFailure(ctx, item.id, item.number, item.attempts, err)
			continue
		}
		if _, err := worker.pool.Exec(ctx, `
			UPDATE orders
			SET cdek_uuid = $2, cdek_synced_at = CURRENT_TIMESTAMP,
					cdek_create_state = 'registered', cdek_last_error = '', cdek_next_attempt_at = NULL
			WHERE id = $1 AND cdek_uuid = ''
		`, item.id, shipment.UUID); err != nil {
			worker.logger.Error("store cdek uuid failed", "error", err, "order", item.number)
		}
	}
}

func (worker *ShippingWorker) recordCreateFailure(
	ctx context.Context,
	orderID int64,
	orderNumber string,
	attempts int,
	failure error,
) {
	// Keep provider details out of customer responses, but retain a bounded
	// diagnostic for managers. Backoff caps at one hour.
	delay := time.Minute * time.Duration(1<<min(6, max(0, attempts-1)))
	message := failure.Error()
	if len(message) > 1000 {
		message = message[:1000]
	}
	state := "retry"
	if errors.Is(failure,integration.ErrCDEKOutcomeUnknown) { state="unknown"; delay=2*time.Minute }
	if _, err := worker.pool.Exec(ctx, `
			UPDATE orders
			SET cdek_create_state = $2, cdek_last_error = $3,
				cdek_next_attempt_at = CURRENT_TIMESTAMP + ($4 * INTERVAL '1 second')
			WHERE id = $1 AND cdek_uuid = ''
		`, orderID, state, message, int(delay/time.Second)); err != nil {
		worker.logger.Error("store cdek failure failed", "error", err, "order", orderNumber)
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
		ORDER BY cdek_synced_at ASC NULLS FIRST, id
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
			if _, err := worker.pool.Exec(ctx, `
				UPDATE orders SET cdek_synced_at = CURRENT_TIMESTAMP WHERE id = $1
			`, item.id); err != nil {
				worker.logger.Error("store cdek sync time failed", "error", err, "order", item.number)
			}
			continue
		}
		nextStatus := item.status
		if mapped, known := cdekStatusToOrder[strings.ToUpper(shipment.Status)]; known {
			nextStatus = mapped
		}
		if _, err := worker.pool.Exec(ctx, `
			UPDATE orders
			SET cdek_track_number = $2, cdek_status = $3, cdek_status_reason = $4, status = $5,
				cdek_synced_at = CURRENT_TIMESTAMP
			WHERE id = $1
		`, item.id, shipment.TrackNumber, shipment.Status, shipment.StatusReason, nextStatus); err != nil {
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

func (worker *ShippingWorker) refreshOfferStatuses(ctx context.Context){
	rows,err:=worker.pool.Query(ctx,`SELECT id,cdek_uuid,cdek_track_number,cdek_status,status FROM shipment_offers WHERE cdek_uuid<>'' AND status NOT IN ('cancelled','completed','expired') ORDER BY cdek_synced_at ASC NULLS FIRST,id LIMIT 50`);if err!=nil{worker.logger.Error("find shipment offers to refresh failed","error",err);return}
	type tracked struct{id int64;uuid,track,cdekStatus,status string};items:=[]tracked{};for rows.Next(){var item tracked;if err:=rows.Scan(&item.id,&item.uuid,&item.track,&item.cdekStatus,&item.status);err!=nil{rows.Close();return};items=append(items,item)};rows.Close()
	for _,item:=range items{shipment,err:=worker.cdek.FetchOrder(ctx,item.uuid);if err!=nil{worker.logger.Error("fetch cdek shipment offer failed","error",err,"offer_id",item.id);continue};next:=item.status;if mapped,ok:=cdekStatusToOrder[strings.ToUpper(shipment.Status)];ok{next=mapped};if _,err:=worker.pool.Exec(ctx,`UPDATE shipment_offers SET cdek_track_number=$2,cdek_status=$3,cdek_status_reason=$4,status=$5,cdek_synced_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP WHERE id=$1`,item.id,shipment.TrackNumber,shipment.Status,shipment.StatusReason,next);err!=nil{worker.logger.Error("store cdek shipment offer status failed","error",err,"offer_id",item.id)}}
}

func (worker *ShippingWorker) shipmentContents(
	ctx context.Context,
	orderID int64,
) ([]integration.ShipmentItem, integration.Parcel, error) {
	rows, err := worker.pool.Query(ctx, `
		SELECT oi.product_name, oi.unit_price::DOUBLE PRECISION, oi.quantity,
			COALESCE(variant_numeric_attribute(pv.id, 'package_length_cm'), 0)::INTEGER, COALESCE(variant_numeric_attribute(pv.id, 'package_width_cm'), 0)::INTEGER,
			COALESCE(variant_numeric_attribute(pv.id, 'package_height_cm'), 0)::INTEGER, COALESCE(variant_numeric_attribute(pv.id, 'package_weight_grams'), 0)::INTEGER
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
