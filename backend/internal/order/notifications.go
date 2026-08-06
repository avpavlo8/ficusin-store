package order

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/jackc/pgx/v5/pgxpool"
)

type NotificationWorker struct {
	pool     *pgxpool.Pool
	notifier Notifier
	logger   *slog.Logger
	interval time.Duration
}

func NewNotificationWorker(
	pool *pgxpool.Pool,
	notifier Notifier,
	logger *slog.Logger,
) *NotificationWorker {
	return &NotificationWorker{
		pool: pool, notifier: notifier, logger: logger, interval: time.Minute,
	}
}

func (worker *NotificationWorker) Run(ctx context.Context) {
	worker.process(ctx)
	ticker := time.NewTicker(worker.interval)
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

func (worker *NotificationWorker) process(ctx context.Context) {
	rows, err := worker.pool.Query(ctx, `
		SELECT id
		FROM orders
		WHERE telegram_notified_at IS NULL
		ORDER BY created_at ASC
		LIMIT 20
	`)
	if err != nil {
		worker.logger.Error("load pending Telegram notifications", "error", err)
		return
	}
	orderIDs := make([]int64, 0, 20)
	for rows.Next() {
		var orderID int64
		if err := rows.Scan(&orderID); err != nil {
			worker.logger.Error("scan pending Telegram notification", "error", err)
			continue
		}
		orderIDs = append(orderIDs, orderID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		worker.logger.Error("read pending Telegram notifications", "error", err)
		return
	}

	for _, orderID := range orderIDs {
		notification, err := worker.load(ctx, orderID)
		if err != nil {
			worker.logger.Error("load Telegram order", "order_id", orderID, "error", err)
			continue
		}
		if err := worker.notifier.SendOrder(ctx, notification); err != nil {
			worker.logger.Error("send Telegram order", "order_id", orderID, "error", err)
			continue
		}
		if _, err := worker.pool.Exec(ctx, `
			UPDATE orders
			SET telegram_notified_at = CURRENT_TIMESTAMP
			WHERE id = $1 AND telegram_notified_at IS NULL
		`, orderID); err != nil {
			worker.logger.Error("mark Telegram order sent", "order_id", orderID, "error", err)
		}
	}
}

func (worker *NotificationWorker) load(
	ctx context.Context,
	orderID int64,
) (integration.TelegramOrder, error) {
	// Only non-personal columns are selected: the notification goes to
	// Telegram, and contacts must not leave our own infrastructure.
	var notification integration.TelegramOrder
	if err := worker.pool.QueryRow(ctx, `
		SELECT
			order_number, delivery_method, COALESCE(cdek_city_name, ''),
			delivery_fee::DOUBLE PRECISION,
			delivery_fee_pending = 1, delivery_repack_requested = 1,
			payment_status, payment_method,
			subtotal::DOUBLE PRECISION, total::DOUBLE PRECISION
		FROM orders
		WHERE id = $1 AND telegram_notified_at IS NULL
	`, orderID).Scan(
		&notification.OrderNumber, &notification.DeliveryMethod,
		&notification.DeliveryCity, &notification.DeliveryFee,
		&notification.DeliveryFeePending, &notification.RepackRequested,
		&notification.PaymentStatus, &notification.PaymentMethod,
		&notification.Subtotal, &notification.Total,
	); err != nil {
		return integration.TelegramOrder{}, fmt.Errorf("load order: %w", err)
	}

	rows, err := worker.pool.Query(ctx, `
		SELECT product_name, unit_price::DOUBLE PRECISION, quantity
		FROM order_items
		WHERE order_id = $1
		ORDER BY id
	`, orderID)
	if err != nil {
		return integration.TelegramOrder{}, fmt.Errorf("load order items: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item integration.TelegramOrderItem
		if err := rows.Scan(&item.Name, &item.Price, &item.Quantity); err != nil {
			return integration.TelegramOrder{}, fmt.Errorf("scan order item: %w", err)
		}
		notification.Items = append(notification.Items, item)
	}
	if err := rows.Err(); err != nil {
		return integration.TelegramOrder{}, fmt.Errorf("read order items: %w", err)
	}
	return notification, nil
}
