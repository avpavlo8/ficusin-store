package order

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/avpavlo8/ficusin-store/backend/internal/payment"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type measuredOrderBox struct {
	LengthCM    int `json:"lengthCm"`
	WidthCM     int `json:"widthCm"`
	HeightCM    int `json:"heightCm"`
	WeightGrams int `json:"weightGrams"`
	Contents    []struct {
		OrderItemID int64 `json:"orderItemId"`
		Quantity    int   `json:"quantity"`
	} `json:"contents"`
}

// The saved physical boxes override missing per-product dimensions. Every unit
// belongs to precisely one box; prices still come from the historical order.
func (worker *ShippingWorker) measuredOrderPackages(ctx context.Context, orderID int64, number string) ([]integration.ShipmentPackage, error) {
	var raw []byte
	err := worker.pool.QueryRow(ctx, `SELECT w.boxes FROM order_packing_workflows w JOIN orders o ON o.id=w.order_id WHERE w.order_id=$1 AND w.order_revision=o.shipment_revision AND w.state NOT IN ('cancelled','stale')`, orderID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	boxes := []measuredOrderBox{}
	if err = json.Unmarshal(raw, &boxes); err != nil {
		return nil, err
	}
	rows, err := worker.pool.Query(ctx, `SELECT id,product_name,unit_price::double precision,quantity FROM order_items WHERE order_id=$1`, orderID)
	if err != nil {
		return nil, err
	}
	items := map[int64]integration.ShipmentItem{}
	for rows.Next() {
		var id int64
		var item integration.ShipmentItem
		if err = rows.Scan(&id, &item.Name, &item.Price, &item.Quantity); err != nil {
			rows.Close()
			return nil, err
		}
		items[id] = item
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return measuredPackages(number, boxes, items)
}

func measuredPackages(number string, boxes []measuredOrderBox, items map[int64]integration.ShipmentItem) ([]integration.ShipmentPackage, error) {
	if len(boxes) == 0 {
		return nil, errors.New("фактические коробки не сохранены")
	}
	packed := map[int64]int{}
	result := make([]integration.ShipmentPackage, 0, len(boxes))
	for index, box := range boxes {
		if box.LengthCM <= 0 || box.WidthCM <= 0 || box.HeightCM <= 0 || box.WeightGrams <= 0 || len(box.Contents) == 0 {
			return nil, errors.New("размеры, вес или состав коробки не заполнены")
		}
		count := 0
		for _, content := range box.Contents {
			count += content.Quantity
		}
		lines := []integration.ShipmentItem{}
		for _, content := range box.Contents {
			item, ok := items[content.OrderItemID]
			if !ok || content.Quantity <= 0 {
				return nil, errors.New("состав заказа изменился после упаковки")
			}
			packed[content.OrderItemID] += content.Quantity
			item.Quantity = content.Quantity
			item.WeightGrams = max(1, box.WeightGrams/max(1, count))
			lines = append(lines, item)
		}
		result = append(result, integration.ShipmentPackage{Number: fmt.Sprintf("%s-%d", number, index+1), Box: integration.Parcel{LengthCM: box.LengthCM, WidthCM: box.WidthCM, HeightCM: box.HeightCM, WeightGrams: box.WeightGrams}, Items: lines})
	}
	for id, item := range items {
		if packed[id] != item.Quantity {
			return nil, errors.New("упаковка не соответствует полному составу заказа")
		}
	}
	return result, nil
}

type packingPayments interface {
	Configured() bool
	BalanceForOrder(context.Context, int64) (payment.Balance, error)
	StartOutstandingForOrderID(context.Context, int64) (string, payment.Balance, error)
}

// PackingWorker only advances durable preparation requests. Carrier creation is
// owned by ShippingWorker, with its existing claim and ambiguous-outcome recovery.
type PackingWorker struct {
	pool            *pgxpool.Pool
	payments        packingPayments
	siteURL         string
	paymentsEnabled func() bool
	logger          *slog.Logger
}

func NewPackingWorker(pool *pgxpool.Pool, payments packingPayments, siteURL string, logger *slog.Logger) *PackingWorker {
	return &PackingWorker{pool: pool, payments: payments, siteURL: strings.TrimRight(siteURL, "/"), logger: logger}
}
func (worker *PackingWorker) WithPaymentsEnabled(enabled func() bool) *PackingWorker {
	worker.paymentsEnabled = enabled
	return worker
}
func (worker *PackingWorker) Run(ctx context.Context) {
	worker.process(ctx)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			worker.process(ctx)
		}
	}
}
func (worker *PackingWorker) process(ctx context.Context) {
	rows, err := worker.pool.Query(ctx, `WITH picked AS(SELECT id FROM order_packing_workflows WHERE state IN ('queued','waiting_cdek','preparing_payment') AND (locked_until IS NULL OR locked_until<CURRENT_TIMESTAMP) AND (next_attempt_at IS NULL OR next_attempt_at<=CURRENT_TIMESTAMP) ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 20) UPDATE order_packing_workflows w SET locked_until=CURRENT_TIMESTAMP+INTERVAL '5 minutes' FROM picked WHERE w.id=picked.id RETURNING w.id`)
	if err != nil {
		worker.logger.Error("claim order preparation failed", "error", err)
		return
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if err = worker.advance(ctx, id); err != nil {
			worker.logger.Error("prepare order notification failed", "error", err, "packing_id", id)
			_, _ = worker.pool.Exec(ctx, `UPDATE order_packing_workflows SET last_error=$2,locked_until=NULL,next_attempt_at=CURRENT_TIMESTAMP+INTERVAL '1 minute',updated_at=CURRENT_TIMESTAMP WHERE id=$1 AND state IN ('queued','waiting_cdek','preparing_payment')`, id, err.Error())
		}
	}
}

func (worker *PackingWorker) advance(ctx context.Context, id int64) error {
	var orderID int64
	var number, email, status, cdekState, track, cdekError, payee string
	var fee float64
	var revision, savedRevision int
	err := worker.pool.QueryRow(ctx, `SELECT o.id,o.order_number,COALESCE(o.email,''),o.status,o.cdek_create_state,COALESCE(o.cdek_track_number,''),o.cdek_last_error,o.shipment_revision,w.order_revision,o.delivery_payee,o.delivery_fee::double precision FROM order_packing_workflows w JOIN orders o ON o.id=w.order_id WHERE w.id=$1`, id).Scan(&orderID, &number, &email, &status, &cdekState, &track, &cdekError, &revision, &savedRevision, &payee, &fee)
	if err != nil {
		return err
	}
	if status == "cancelled" || revision != savedRevision {
		_, err = worker.pool.Exec(ctx, `UPDATE order_packing_workflows SET state='stale',locked_until=NULL WHERE id=$1`, id)
		return err
	}
	if cdekState != "registered" || track == "" {
		_, err = worker.pool.Exec(ctx, `UPDATE order_packing_workflows SET state='waiting_cdek',last_error=$2,locked_until=NULL,next_attempt_at=CURRENT_TIMESTAMP+INTERVAL '15 seconds',updated_at=CURRENT_TIMESTAMP WHERE id=$1 AND state IN ('queued','waiting_cdek','preparing_payment')`, id, cdekError)
		return err
	}
	if _, err = worker.pool.Exec(ctx, `UPDATE order_packing_workflows SET state='preparing_payment' WHERE id=$1 AND state IN ('queued','waiting_cdek','preparing_payment')`, id); err != nil {
		return err
	}
	if worker.paymentsEnabled != nil && !worker.paymentsEnabled() {
		return errors.New("онлайн-оплата выключена в настройках; подготовка приостановлена")
	}
	if worker.payments == nil || !worker.payments.Configured() {
		return errors.New("онлайн-оплата не настроена; подготовка приостановлена")
	}
	balance, err := worker.payments.BalanceForOrder(ctx, orderID)
	if err != nil {
		return err
	}
	paymentURL := ""
	if balance.Due > 0 {
		paymentURL, balance, err = worker.payments.StartOutstandingForOrderID(ctx, orderID)
		if err != nil {
			return err
		}
		parsed, parseErr := url.Parse(paymentURL)
		if parseErr != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return errors.New("платёжный сервис ещё не выдал ссылку; подготовка приостановлена")
		}
	}
	tx, err := worker.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Recheck after external payment calls: cancellation wins over notification.
	if err = tx.QueryRow(ctx, `SELECT status,shipment_revision FROM orders WHERE id=$1 FOR UPDATE`, orderID).Scan(&status, &revision); err != nil {
		return err
	}
	if status == "cancelled" || revision != savedRevision {
		return errors.New("заказ изменился; уведомление не отправлено")
	}
	var workflowState string
	if err = tx.QueryRow(ctx, `SELECT state FROM order_packing_workflows WHERE id=$1 FOR UPDATE`, id).Scan(&workflowState); err != nil {
		return err
	}
	if workflowState != "queued" && workflowState != "waiting_cdek" && workflowState != "preparing_payment" {
		return tx.Commit(ctx)
	}
	body := fmt.Sprintf("Упаковка заказа %s подтверждена.\n\nТрек-номер СДЭК: %s\nОтследить: https://www.cdek.ru/ru/tracking?order_id=%s\nРегистрация заявки ещё не означает передачу посылки перевозчику.\n", number, track, url.QueryEscape(track))
	if payee == "carrier" {
		body += fmt.Sprintf("\nДоставка оплачивается перевозчику отдельно. Расчётная стоимость доставки: %.2f ₽.\n", fee)
	}
	if balance.Due > 0 {
		body += fmt.Sprintf("\nК оплате: %.2f ₽.\nОплатить заказ: %s\n", balance.Due, paymentURL)
	} else {
		body += "\nЗаказ полностью оплачен.\n"
	}
	body += "\nВаш заказ: " + worker.siteURL + "/account/orders/" + url.PathEscape(number) + "\n\nФикусин"
	if email != "" {
		if _, err = tx.Exec(ctx, `INSERT INTO outbox(recipient,subject,body,order_packing_id) VALUES($1,$2,$3,$4) ON CONFLICT(order_packing_id) WHERE order_packing_id IS NOT NULL DO NOTHING`, email, "Заказ "+number+" подготовлен к оплате и отправке", body, id); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE orders SET cdek_tracking_notified_at=COALESCE(cdek_tracking_notified_at,CURRENT_TIMESTAMP) WHERE id=$1`, orderID); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO order_packing_push_outbox(workflow_id,subscription_id) SELECT $1,s.id FROM push_subscriptions s JOIN orders o ON o.customer_id=s.customer_id WHERE o.id=$2 AND s.expired_at IS NULL ON CONFLICT DO NOTHING`, id, orderID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE order_packing_workflows SET state='notification_queued',prepared_at=CURRENT_TIMESTAMP,payment_url=$2,last_error='',locked_until=NULL,next_attempt_at=NULL,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, id, paymentURL); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
