package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/avpavlo8/ficusin-store/backend/internal/money"
	"math"
	"strings"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/avpavlo8/ficusin-store/backend/internal/settings"
	"github.com/jackc/pgx/v5"
)

type packingSettings interface{ Enabled(string) bool }

func (repository *PostgresRepository) OrderForPacking(ctx context.Context, id int64) (Order, error) {
	return currentAdminOrder(ctx, repository, id)
}

type OrderPackingInput struct {
	Revision          int                `json:"revision"`
	CityCode          int                `json:"cityCode,omitempty"`
	OfficeCode        string             `json:"officeCode,omitempty"`
	Boxes             []ShipmentBoxInput `json:"boxes"`
	TariffCode        int                `json:"tariffCode"`
	QuotedDeliveryFee *float64           `json:"quotedDeliveryFee"`
	IdempotencyKey    string             `json:"idempotencyKey"`
}
type PackingNotification struct {
	State    string     `json:"state"`
	Attempts int        `json:"attempts"`
	SentAt   *time.Time `json:"sentAt,omitempty"`
	Error    string     `json:"error"`
}
type OrderPackingState struct {
	Revision      int                 `json:"revision"`
	Boxes         []ShipmentBoxInput  `json:"boxes"`
	TariffCode    int                 `json:"tariffCode"`
	TariffName    string              `json:"tariffName"`
	DeliveryFee   float64             `json:"deliveryFee"`
	DeliveryPayee string              `json:"deliveryPayee"`
	OfficeCode    string              `json:"officeCode"`
	CityCode      int                 `json:"cityCode"`
	State         string              `json:"state"`
	CDEKState     string              `json:"cdekState"`
	TrackNumber   string              `json:"trackNumber"`
	PaymentURL    string              `json:"paymentUrl"`
	Error         string              `json:"error"`
	Notification  PackingNotification `json:"notification"`
	Blockers      []string            `json:"blockers"`
	QuoteBlockers []string            `json:"quoteBlockers"`
	Push          PackingNotification `json:"push"`
}

func (repository *PostgresRepository) WithOrderPacking(shop packingSettings, mailConfigured, paymentConfigured bool, wake func()) *PostgresRepository {
	repository.packingSettings = shop
	repository.packingMailConfigured = mailConfigured
	repository.packingPaymentConfigured = paymentConfigured
	repository.packingWake = wake
	return repository
}

func (repository *PostgresRepository) OrderPacking(ctx context.Context, id int64) (OrderPackingState, error) {
	state := OrderPackingState{Boxes: []ShipmentBoxInput{}, State: "draft", Notification: PackingNotification{State: "not_queued"}, Blockers: []string{}, QuoteBlockers: []string{}, Push: PackingNotification{State: "unavailable", Error: "Покупатель не включил push-уведомления"}}
	var delivery, status, method, email, uuid, lastError string
	var preorder, activeOffers bool
	err := repository.pool.QueryRow(ctx, `SELECT shipment_revision,COALESCE(cdek_tariff_code,0),delivery_fee::double precision,delivery_payee,COALESCE(cdek_office_code,''),COALESCE(cdek_city_code,0),cdek_create_state,COALESCE(cdek_track_number,''),delivery_method,status,payment_method,COALESCE(email,''),cdek_uuid,cdek_last_error,has_preorder=1,
 EXISTS(SELECT 1 FROM shipment_offers WHERE order_id=orders.id AND status NOT IN ('draft','packaging_required','cancelled','expired','stale')) FROM orders WHERE id=$1`, id).Scan(&state.Revision, &state.TariffCode, &state.DeliveryFee, &state.DeliveryPayee, &state.OfficeCode, &state.CityCode, &state.CDEKState, &state.TrackNumber, &delivery, &status, &method, &email, &uuid, &lastError, &preorder, &activeOffers)
	if err != nil {
		return state, err
	}
	if delivery != "cdek" {
		state.Blockers = append(state.Blockers, "Для этого заказа выбрана не доставка СДЭК")
	}
	if state.OfficeCode == "" {
		state.Blockers = append(state.Blockers, "В заказе не указан пункт выдачи СДЭК")
	}
	if state.CityCode <= 0 {
		state.Blockers = append(state.Blockers, "В заказе не указан город СДЭК")
	}
	if status == "cancelled" || status == "completed" || status == "shipped" {
		state.Blockers = append(state.Blockers, "Заказ закрыт или уже передан в доставку")
	}
	if method != "online" {
		state.Blockers = append(state.Blockers, "Автоматическая ссылка доступна только для заказа с оплатой картой на сайте")
	}
	if state.DeliveryPayee != "shop" {
		state.Blockers = append(state.Blockers, "Доставка оплачивается перевозчику: сначала подтвердите такой договор СДЭК; автоматическая подготовка не меняет плательщика")
	}
	if preorder {
		state.Blockers = append(state.Blockers, "Сначала подтвердите наличие всех растений и сохраните состав заказа")
	}
	if email == "" {
		state.Notification = PackingNotification{State: "unavailable", Error: "Email покупателя не указан"}
	}
	if activeOffers {
		state.Blockers = append(state.Blockers, "Есть отправка, уже предложенная покупателю или оплаченная; сначала завершите её обработку")
	}
	if repository.shipmentQuotes == nil || !repository.shipmentQuotes.Configured() {
		state.Blockers = append(state.Blockers, "СДЭК не настроен")
	}
	if repository.packingSettings == nil || !repository.packingSettings.Enabled(settings.CDEKOrdersEnabled) {
		state.Blockers = append(state.Blockers, "Автоматическое создание заявок СДЭК выключено в настройках")
	}
	if !repository.packingMailConfigured {
		state.Notification = PackingNotification{State: "unavailable", Error: "Отправка писем не настроена"}
	}
	if repository.packingSettings == nil || !repository.packingSettings.Enabled(settings.PaymentsEnabled) {
		state.Blockers = append(state.Blockers, "Онлайн-оплата выключена в настройках")
	}
	if !repository.packingPaymentConfigured {
		state.Blockers = append(state.Blockers, "Онлайн-оплата не настроена")
	}
	for _, blocker := range state.Blockers {
		switch blocker {
		case "Для этого заказа выбрана не доставка СДЭК", "В заказе не указан пункт выдачи СДЭК", "В заказе не указан город СДЭК", "Заказ закрыт или уже передан в доставку", "СДЭК не настроен":
			state.QuoteBlockers = append(state.QuoteBlockers, blocker)
		}
	}
	var subscribed bool
	if err = repository.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM push_subscriptions s JOIN orders o ON o.customer_id=s.customer_id WHERE o.id=$1 AND s.expired_at IS NULL)`, id).Scan(&subscribed); err != nil {
		return state, err
	}
	if sender, ok := repository.notifier.(interface{ PublicKey() string }); ok && sender.PublicKey() == "" {
		state.Push.Error = "Push выключен: не настроены ключи VAPID"
		subscribed = false
	}
	if subscribed {
		state.Push = PackingNotification{State: "not_queued"}
	}
	var raw []byte
	var attempts int
	var cancelledAt *time.Time
	err = repository.pool.QueryRow(ctx, `SELECT w.boxes,w.tariff_code,w.tariff_name,w.state,w.payment_url,w.last_error,b.sent_at,b.cancelled_at,COALESCE(b.attempts,0),COALESCE(b.last_error,''),b.id IS NOT NULL FROM order_packing_workflows w LEFT JOIN outbox b ON b.order_packing_id=w.id WHERE w.order_id=$1 AND w.state NOT IN ('cancelled','stale') ORDER BY w.id DESC LIMIT 1`, id).Scan(&raw, &state.TariffCode, &state.TariffName, &state.State, &state.PaymentURL, &state.Error, &state.Notification.SentAt, &cancelledAt, &attempts, &state.Notification.Error, &activeOffers)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return state, err
	}
	if err == nil {
		if err = json.Unmarshal(raw, &state.Boxes); err != nil {
			return state, err
		}
		state.Notification.Attempts = attempts
		switch {
		case state.Notification.SentAt != nil:
			state.Notification.State = "sent"
		case cancelledAt != nil || attempts >= 5:
			state.Notification.State = "failed"
		case activeOffers:
			state.Notification.State = "queued"
		}
	} else if uuid != "" || state.CDEKState == "creating" || state.CDEKState == "unknown" || state.CDEKState == "registered" {
		state.Blockers = append(state.Blockers, "Заявка СДЭК уже создаётся или зарегистрирована; сначала сверьте её у перевозчика")
	}
	if state.State != "draft" && state.Error == "" {
		state.Error = lastError
	}
	// A historical missing-tariff failure is resolved by the current quote flow.
	if state.State == "draft" && (uuid != "" || state.CDEKState == "unknown" || state.CDEKState == "creating" || state.CDEKState == "registered") {
		state.QuoteBlockers = append(state.QuoteBlockers, "Заявка СДЭК уже создаётся или зарегистрирована; сначала сверьте её у перевозчика")
	}
	var queued, sent, failed int
	var pushError string
	err = repository.pool.QueryRow(ctx, `SELECT COUNT(*) FILTER(WHERE p.sent_at IS NULL AND p.cancelled_at IS NULL AND p.attempts<5),COUNT(*) FILTER(WHERE p.sent_at IS NOT NULL),COUNT(*) FILTER(WHERE p.cancelled_at IS NOT NULL OR p.attempts>=5),COALESCE(MAX(p.last_error),'') FROM order_packing_push_outbox p JOIN order_packing_workflows w ON w.id=p.workflow_id WHERE w.order_id=$1 AND w.state NOT IN ('cancelled','stale')`, id).Scan(&queued, &sent, &failed, &pushError)
	if err != nil {
		return state, err
	}
	switch {
	case queued > 0:
		state.Push = PackingNotification{State: "queued", Error: pushError}
	case sent > 0:
		state.Push = PackingNotification{State: "sent"}
	case failed > 0:
		state.Push = PackingNotification{State: "failed", Error: pushError}
	}
	if state.Notification.State != "sent" && (!repository.packingMailConfigured || email == "") {
		state.Notification.State = "unavailable"
		if !repository.packingMailConfigured {
			state.Notification.Error = "Отправка писем не настроена"
		} else {
			state.Notification.Error = "Email покупателя не указан"
		}
	}

	return state, nil
}

func validatePackingSelection(boxes []ShipmentBoxInput, items []ShipmentOfferLineInput) error {
	if len(items) == 0 {
		return errors.New("в заказе нет растений для упаковки")
	}
	if len(boxes) > len(items) {
		return errors.New("коробок не может быть больше, чем позиций в заказе")
	}
	if len(boxes) > 20 {
		return errors.New("в одной отправке допускается не более 20 коробок")
	}
	if err := validateShipmentBoxes(boxes); err != nil {
		return err
	}
	for _, box := range boxes {
		if len(box.Contents) == 0 {
			return errors.New("распределите растения по коробкам и уберите пустые коробки")
		}
		if box.LengthCM > 300 || box.WidthCM > 300 || box.HeightCM > 300 || box.WeightGrams > 200000 {
			return errors.New("проверьте размеры и вес коробки")
		}
	}
	_, err := shipmentSelection(items, boxes)
	return err
}

func (repository *PostgresRepository) packingItems(ctx context.Context, id int64) ([]ShipmentOfferLineInput, error) {
	rows, err := repository.pool.Query(ctx, `SELECT id,quantity FROM order_items WHERE order_id=$1 ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ShipmentOfferLineInput{}
	for rows.Next() {
		var item ShipmentOfferLineInput
		if err := rows.Scan(&item.OrderItemID, &item.Quantity); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (repository *PostgresRepository) packingDestination(ctx context.Context, state OrderPackingState, input OrderPackingInput) (int, string, string, string, error) {
	city, office := state.CityCode, state.OfficeCode
	if input.CityCode != 0 || input.OfficeCode != "" {
		city = input.CityCode
		office = strings.TrimSpace(input.OfficeCode)
	}
	if city <= 0 || office == "" {
		return 0, "", "", "", errors.New("выберите город и пункт выдачи СДЭК")
	}
	service, ok := repository.shipmentQuotes.(interface {
		GetOffices(context.Context, int) ([]integration.CDEKOffice, error)
	})
	if !ok {
		return 0, "", "", "", errors.New("проверка пункта СДЭК недоступна")
	}
	offices, err := service.GetOffices(ctx, city)
	if err != nil {
		return 0, "", "", "", fmt.Errorf("не удалось проверить пункт СДЭК: %w", err)
	}
	for _, candidate := range offices {
		if candidate.Code == office {
			address := candidate.Location.AddressFull
			if address == "" {
				address = candidate.Location.Address
			}
			if address == "" {
				address = "Пункт выдачи СДЭК " + office
			}
			return city, office, address, candidate.Location.City, nil
		}
	}
	return 0, "", "", "", errors.New("пункт выдачи не найден в выбранном городе — выберите действующий пункт")
}

func (repository *PostgresRepository) QuoteOrderPacking(ctx context.Context, actor Actor, id int64, input OrderPackingInput) (ShipmentOfferQuote, error) {
	if !Can(actor.Role, PermissionOrdersEdit) {
		return ShipmentOfferQuote{}, ErrForbidden
	}
	state, err := repository.OrderPacking(ctx, id)
	if err != nil {
		return ShipmentOfferQuote{}, err
	}
	if state.Revision != input.Revision {
		return ShipmentOfferQuote{}, errors.New("заказ изменился — обновите карточку перед расчётом")
	}
	if input.CityCode > 0 && input.OfficeCode != "" {
		filtered := []string{}
		for _, blocker := range state.QuoteBlockers {
			if blocker != "В заказе не указан пункт выдачи СДЭК" && blocker != "В заказе не указан город СДЭК" {
				filtered = append(filtered, blocker)
			}
		}
		state.QuoteBlockers = filtered
	}
	if len(state.QuoteBlockers) > 0 {
		return ShipmentOfferQuote{}, errors.New(strings.Join(state.QuoteBlockers, ". "))
	}
	if state.State != "draft" {
		return ShipmentOfferQuote{}, errors.New("упаковка уже сохранена и передана в обработку")
	}
	items, err := repository.packingItems(ctx, id)
	if err != nil {
		return ShipmentOfferQuote{}, err
	}
	if err = validatePackingSelection(input.Boxes, items); err != nil {
		return ShipmentOfferQuote{}, err
	}
	city, _, _, _, err := repository.packingDestination(ctx, state, input)
	if err != nil {
		return ShipmentOfferQuote{}, err
	}
	quotes, err := repository.shipmentQuotes.CalculatePVZPackages(ctx, city, shipmentParcels(input.Boxes))
	return ShipmentOfferQuote{Quotes: quotes}, err
}

func packingFingerprint(input OrderPackingInput) string {
	input.IdempotencyKey = ""
	raw, _ := json.Marshal(input)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (repository *PostgresRepository) FinalizeOrderPacking(ctx context.Context, actor Actor, id int64, input OrderPackingInput) (OrderPackingState, error) {
	if !Can(actor.Role, PermissionOrdersEdit) {
		return OrderPackingState{}, ErrForbidden
	}
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.QuotedDeliveryFee == nil || math.IsNaN(*input.QuotedDeliveryFee) || math.IsInf(*input.QuotedDeliveryFee, 0) || *input.QuotedDeliveryFee < 0 {
		return OrderPackingState{}, errors.New("сначала рассчитайте доставку и подтвердите показанную стоимость")
	}
	if len(input.IdempotencyKey) < 8 || len(input.IdempotencyKey) > 100 {
		return OrderPackingState{}, errors.New("нужен ключ повторяемой операции")
	}
	fingerprint := packingFingerprint(input)
	var oldFingerprint string
	err := repository.pool.QueryRow(ctx, `SELECT fingerprint FROM order_packing_workflows WHERE order_id=$1 AND idempotency_key=$2`, id, input.IdempotencyKey).Scan(&oldFingerprint)
	if err == nil {
		if oldFingerprint != fingerprint {
			return OrderPackingState{}, errors.New("ключ уже использован для другой упаковки")
		}
		return repository.OrderPacking(ctx, id)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return OrderPackingState{}, err
	}
	quoted, err := repository.QuoteOrderPacking(ctx, actor, id, input)
	if err != nil {
		var replay string
		if repository.pool.QueryRow(ctx, `SELECT fingerprint FROM order_packing_workflows WHERE order_id=$1 AND idempotency_key=$2`, id, input.IdempotencyKey).Scan(&replay) == nil && replay == fingerprint {
			return repository.OrderPacking(ctx, id)
		}
		return OrderPackingState{}, err
	}
	var chosen *integration.CDEKQuote
	for index := range quoted.Quotes {
		if quoted.Quotes[index].TariffCode == input.TariffCode {
			chosen = &quoted.Quotes[index]
			break
		}
	}
	if chosen == nil {
		return OrderPackingState{}, errors.New("выберите доступный тариф из расчёта СДЭК")
	}
	if money.Kopecks(*input.QuotedDeliveryFee) != int64(chosen.Price)*100 {
		return OrderPackingState{}, errors.New("стоимость тарифа СДЭК изменилась — пересчитайте доставку и подтвердите новую сумму")
	}
	currentState, err := repository.OrderPacking(ctx, id)
	if err != nil {
		return OrderPackingState{}, err
	}
	finalBlockers := []string{}
	for _, blocker := range currentState.Blockers {
		if input.CityCode > 0 && input.OfficeCode != "" && (blocker == "В заказе не указан пункт выдачи СДЭК" || blocker == "В заказе не указан город СДЭК") {
			continue
		}
		finalBlockers = append(finalBlockers, blocker)
	}
	if len(finalBlockers) > 0 {
		return OrderPackingState{}, errors.New(strings.Join(finalBlockers, ". "))
	}
	selectedCity, selectedOffice, selectedAddress, selectedCityName, err := repository.packingDestination(ctx, currentState, input)
	if err != nil {
		return OrderPackingState{}, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return OrderPackingState{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Serialize preparation retries before examining or retiring payment attempts.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7241,$1::integer)`, id); err != nil {
		return OrderPackingState{}, err
	}
	err = tx.QueryRow(ctx, `SELECT fingerprint FROM order_packing_workflows WHERE order_id=$1 AND idempotency_key=$2`, id, input.IdempotencyKey).Scan(&oldFingerprint)
	if err == nil {
		if oldFingerprint != fingerprint {
			return OrderPackingState{}, errors.New("ключ уже использован для другой упаковки")
		}
		_ = tx.Rollback(ctx)
		return repository.OrderPacking(ctx, id)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return OrderPackingState{}, err
	}
	// The same payment locks are taken by capture. No provider request occurs in this transaction.
	pending, err := tx.Query(ctx, `SELECT id FROM payments WHERE order_id=$1 AND status='pending' ORDER BY id`, id)
	if err != nil {
		return OrderPackingState{}, err
	}
	paymentIDs := []int64{}
	for pending.Next() {
		var value int64
		if err = pending.Scan(&value); err != nil {
			pending.Close()
			return OrderPackingState{}, err
		}
		paymentIDs = append(paymentIDs, value)
	}
	pending.Close()
	if err = pending.Err(); err != nil {
		return OrderPackingState{}, err
	}
	for _, paymentID := range paymentIDs {
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, -paymentID); err != nil {
			return OrderPackingState{}, err
		}
	}
	var revision int
	var status, delivery, method, payee, office, email, uuid, cdekState string
	var city int
	var preorder bool
	var subtotal, oldFee float64
	err = tx.QueryRow(ctx, `SELECT shipment_revision,status,delivery_method,payment_method,delivery_payee,COALESCE(cdek_office_code,''),COALESCE(email,''),cdek_uuid,cdek_create_state,COALESCE(cdek_city_code,0),has_preorder=1,subtotal::double precision,delivery_fee::double precision FROM orders WHERE id=$1 FOR UPDATE`, id).Scan(&revision, &status, &delivery, &method, &payee, &office, &email, &uuid, &cdekState, &city, &preorder, &subtotal, &oldFee)
	if err != nil {
		return OrderPackingState{}, err
	}
	if revision != input.Revision {
		return OrderPackingState{}, errors.New("заказ изменился — обновите карточку")
	}
	if status == "cancelled" || status == "completed" || status == "shipped" || delivery != "cdek" || method != "online" || payee != "shop" || preorder {
		return OrderPackingState{}, errors.New("условия заказа изменились — обновите карточку и проверьте причины")
	}
	if uuid != "" || cdekState == "creating" || cdekState == "unknown" || cdekState == "registered" {
		return OrderPackingState{}, errors.New("заявка СДЭК уже создаётся или зарегистрирована; повторное создание запрещено")
	}
	var conflict bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM order_packing_workflows WHERE order_id=$1 AND state NOT IN ('cancelled','stale')) OR EXISTS(SELECT 1 FROM shipment_offers WHERE order_id=$1 AND status NOT IN ('draft','packaging_required','cancelled','expired','stale'))`, id).Scan(&conflict); err != nil {
		return OrderPackingState{}, err
	}
	if conflict {
		return OrderPackingState{}, errors.New("заказ уже передан в обработку или имеет активную частичную отправку")
	}
	var short bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM order_items oi WHERE order_id=$1 GROUP BY variant_id HAVING variant_id IS NULL OR SUM(quantity)>COALESCE((SELECT SUM(GREATEST(i.available_qty-i.reserved_qty,0)) FROM inventory i JOIN warehouses w ON w.id=i.warehouse_id AND w.saby_id='saby-ryazan-main' AND w.is_active=1 JOIN product_variants v ON v.id=i.variant_id JOIN products p ON p.id=v.product_id AND 'stock'=ANY(p.saby_fields) JOIN saby_nomenclature n ON n.saby_id=v.saby_id AND n.missing_since IS NULL WHERE i.variant_id=oi.variant_id AND i.synced_at>=CURRENT_TIMESTAMP-INTERVAL '2 hours'),0))`, id).Scan(&short); err != nil {
		return OrderPackingState{}, err
	}
	if short {
		return OrderPackingState{}, errors.New("свежее наличие СБИС не подтверждает весь состав — проверьте растения")
	}
	if _, err = tx.Exec(ctx, `UPDATE shipment_offers SET status='stale',updated_at=CURRENT_TIMESTAMP WHERE order_id=$1 AND status IN ('draft','packaging_required')`, id); err != nil {
		return OrderPackingState{}, err
	}
	fee := float64(chosen.Price)
	if payee == "shop" && money.Kopecks(fee) != money.Kopecks(oldFee) {
		var legacyPayment bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM payments WHERE order_id=$1 AND status='pending' AND (provider_payment_id<>'' OR request_payload IS NOT NULL) AND COALESCE(request_payload->'Metadata'->>'ficusin_capture_mode',request_payload->'metadata'->>'ficusin_capture_mode','')<>'two_stage')`, id).Scan(&legacyPayment); err != nil {
			return OrderPackingState{}, err
		}
		if legacyPayment {
			return OrderPackingState{}, errors.New("есть действующая платёжная ссылка старого типа — сначала сверьте платёж, чтобы не выставить две разные суммы")
		}
		if _, err = tx.Exec(ctx, `UPDATE payments SET status='superseded',updated_at=CURRENT_TIMESTAMP WHERE order_id=$1 AND status='pending'`, id); err != nil {
			return OrderPackingState{}, err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE orders SET status=CASE WHEN status='new' THEN 'confirmed' ELSE status END,delivery_fee=$2,total=subtotal+CASE WHEN delivery_payee='shop' THEN $2::numeric ELSE 0 END,delivery_fee_pending=0,delivery_repack_requested=0,cdek_tariff_code=$3,cdek_checkout_requested=1,cdek_create_state='new',cdek_next_attempt_at=NULL,cdek_last_error='',cdek_city_code=$4,cdek_office_code=$5,address=$6,cdek_city_name=$7 WHERE id=$1`, id, fee, chosen.TariffCode, selectedCity, selectedOffice, selectedAddress, selectedCityName); err != nil {
		return OrderPackingState{}, err
	}
	// Delivery may create a new amount due even when the plants were paid.
	// Keep operational guards consistent with the same immutable money facts.
	if _, err = tx.Exec(ctx, `WITH money AS (
 SELECT GREATEST(COALESCE((SELECT SUM(amount) FROM payments WHERE order_id=$1 AND status='paid'),0)-COALESCE((SELECT SUM(amount) FROM payment_refunds WHERE order_id=$1 AND status='succeeded'),0),0) AS net,
 COALESCE((SELECT SUM(amount) FROM payment_refunds WHERE order_id=$1 AND status='succeeded'),0) AS refunded
 ) UPDATE orders SET payment_status=CASE WHEN total>0 AND money.net=total THEN 'paid' WHEN money.net>0 THEN 'partially_paid' WHEN money.refunded>0 AND total=0 THEN 'refunded' ELSE 'pending' END FROM money WHERE id=$1`, id); err != nil {
		return OrderPackingState{}, err
	}
	raw, _ := json.Marshal(input.Boxes)
	if _, err = tx.Exec(ctx, `INSERT INTO order_packing_workflows(order_id,order_revision,idempotency_key,fingerprint,boxes,tariff_code,tariff_name,delivery_fee) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, id, revision, input.IdempotencyKey, fingerprint, raw, chosen.TariffCode, chosen.TariffName, fee); err != nil {
		return OrderPackingState{}, err
	}
	if err = insertAudit(ctx, tx, actor, "order.packing.finalize", "order", fmt.Sprint(id), nil, map[string]any{"revision": revision, "boxes": input.Boxes, "tariffCode": chosen.TariffCode, "deliveryFee": fee, "subtotal": subtotal}); err != nil {
		return OrderPackingState{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return OrderPackingState{}, err
	}
	if repository.packingWake != nil {
		repository.packingWake()
	}
	return repository.OrderPacking(ctx, id)
}
