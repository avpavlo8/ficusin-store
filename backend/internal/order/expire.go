package order

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// settingsReader is the panel switch for how long an unpaid order waits.
type settingsReader interface {
	Number(key string) int
}

// paymentCanceller закрывает неоплаченный платёж на стороне провайдера.
//
// Нужен именно здесь: отмена заказа без отмены платежа оставляет
// покупателю рабочую ссылку на оплату заказа, которого больше нет.
type paymentCanceller interface {
	CancelPending(ctx context.Context, orderID int64) error
}

// ExpiryWorker cancels orders nobody paid for.
//
// An order holds its plants in reserve from the moment it is placed. Without
// this the shop slowly fills with abandoned baskets holding the last copy of
// something, and a customer who would have paid is told it is out of stock.
type ExpiryWorker struct {
	pool     *pgxpool.Pool
	settings settingsReader
	payments paymentCanceller
	logger   *slog.Logger
	interval time.Duration
}

func NewExpiryWorker(
	pool *pgxpool.Pool,
	shopSettings settingsReader,
	payments paymentCanceller,
	logger *slog.Logger,
) *ExpiryWorker {
	return &ExpiryWorker{
		pool:     pool,
		settings: shopSettings,
		payments: payments,
		logger:   logger,
		interval: 10 * time.Minute,
	}
}

func (worker *ExpiryWorker) Run(ctx context.Context) {
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

func (worker *ExpiryWorker) process(ctx context.Context) {
	// The owner sets this in the panel; zero switches expiry off entirely,
	// for a shop that would rather chase every order by hand.
	hours := worker.settings.Number("orders.auto_cancel_hours")
	if hours <= 0 {
		return
	}
	rows, err := worker.pool.Query(ctx, `
		SELECT id FROM orders
		WHERE payment_status = 'pending'
			-- Только брошенная онлайн-оплата. «После подтверждения
			-- менеджером» тоже лежит в pending, и по общему условию заказ
			-- отменялся сам, пока менеджер до него не дошёл: покупатель
			-- ничего не бросал, а заказ исчезал вместе с резервом.
			AND payment_method = 'online'
			AND status NOT IN ('cancelled', 'completed')
			AND created_at < CURRENT_TIMESTAMP - make_interval(hours => $1)
		ORDER BY id
		LIMIT 100
	`, hours)
	if err != nil {
		worker.logger.Error("find expired orders failed", "error", err)
		return
	}
	expired := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			worker.logger.Error("scan expired order failed", "error", err)
			break
		}
		expired = append(expired, id)
	}
	rows.Close()

	for _, id := range expired {
		if err := worker.cancel(ctx, id); err != nil {
			worker.logger.Error("cancel expired order failed", "error", err, "order_id", id)
		}
	}
}

// cancel returns the plants to the shelf and closes the unfinished payment.
// Everything happens in one transaction: stock released without the order
// being cancelled would let the same plants be sold twice.
func (worker *ExpiryWorker) cancel(ctx context.Context, orderID int64) error {
	// Сначала гасим платёж у провайдера — и только потом трогаем заказ.
	//
	// Порядок важен дважды. Во-первых, отказ провайдера означает, что
	// платёж жив или уже оплачен: тогда заказ отменять нельзя, товар
	// остаётся за покупателем, а следующий проход попробует снова.
	// Во-вторых, сетевой запрос идёт до открытия транзакции: под
	// блокировками склада он держал бы чужие оформления.
	if worker.payments != nil {
		if err := worker.payments.CancelPending(ctx, orderID); err != nil {
			return err
		}
	}

	transaction, err := worker.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	if _, err := transaction.Exec(ctx, `
		UPDATE orders
		SET status = 'cancelled', payment_status = 'cancelled'
		WHERE id = $1 AND status <> 'cancelled'
	`, orderID); err != nil {
		return err
	}
	if err := ReleaseStock(ctx, transaction, orderID); err != nil {
		return err
	}
	if _, err := transaction.Exec(ctx, `
		UPDATE payments SET status = 'cancelled', updated_at = CURRENT_TIMESTAMP
		WHERE order_id = $1 AND status = 'pending'
	`, orderID); err != nil {
		return err
	}
	return transaction.Commit(ctx)
}
