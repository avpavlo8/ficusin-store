package order

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Ступень скидки: каждые 10 000 ₽ выполненных заказов — один процент.
const (
	spendPerStepMinor = 1_000_000 // 10 000 ₽ в копейках
	bpsPerStep        = 100       // один процент в базисных пунктах
	maxLoyaltySteps   = 10        // потолок — десять процентов при 100 000 ₽
)

// DiscountBPS — персональная скидка по сумме выполненных заказов.
//
// Ступеньками, а не плавно: «потратили 30 000 — у вас 3%» покупатель
// посчитает в уме и проверит, а дробные проценты выглядят произвольной
// цифрой и порождают вопросы в первый же день.
func DiscountBPS(spendMinor int64) int {
	if spendMinor <= 0 {
		return 0
	}
	steps := spendMinor / spendPerStepMinor
	if steps > maxLoyaltySteps {
		steps = maxLoyaltySteps
	}
	return int(steps) * bpsPerStep
}

// LoyaltyWorker пересчитывает персональную скидку покупателя, когда у него
// появляется новый выполненный заказ.
//
// Отдельным работником, а не крючком в панели: заказ может стать
// выполненным не только руками менеджера, и скидка не должна
// зависеть от того, каким именно путём это произошло.
type LoyaltyWorker struct {
	pool     *pgxpool.Pool
	logger   *slog.Logger
	interval time.Duration
}

func NewLoyaltyWorker(pool *pgxpool.Pool, logger *slog.Logger) *LoyaltyWorker {
	return &LoyaltyWorker{pool: pool, logger: logger, interval: time.Minute}
}

func (worker *LoyaltyWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(worker.interval)
	defer ticker.Stop()
	// Первый проход сразу после старта: иначе заказы, выполненные до
	// выкладки, ждали бы лишнюю минуту ни за что.
	worker.process(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			worker.process(ctx)
		}
	}
}

func (worker *LoyaltyWorker) process(ctx context.Context) {
	rows, err := worker.pool.Query(ctx, `
		SELECT DISTINCT customer_id FROM orders
		WHERE status = 'completed'
			AND customer_id IS NOT NULL
			AND spend_counted_at IS NULL
		ORDER BY customer_id
		LIMIT 100
	`)
	if err != nil {
		worker.logger.Error("find customers for loyalty recount failed", "error", err)
		return
	}
	customers := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			worker.logger.Error("scan customer for loyalty recount failed", "error", err)
			break
		}
		customers = append(customers, id)
	}
	rows.Close()

	for _, id := range customers {
		if err := worker.recount(ctx, id); err != nil {
			worker.logger.Error("loyalty recount failed", "error", err, "customer_id", id)
		}
	}
}

// recount считает сумму заново по всем выполненным заказам покупателя.
//
// Не прибавляет к накопленному, а считает с нуля: если выполненный заказ
// потом отменят, следующий пересчёт сам всё поправит. Накопительный
// счётчик так не умеет: одна ошибка осталась бы в нём навсегда.
func (worker *LoyaltyWorker) recount(ctx context.Context, customerID int64) error {
	transaction, err := worker.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	var spendMinor int64
	if err := transaction.QueryRow(ctx, `
		SELECT COALESCE(SUM(ROUND(total * 100)), 0)::BIGINT
		FROM orders
		WHERE customer_id = $1 AND status = 'completed'
	`, customerID).Scan(&spendMinor); err != nil {
		return err
	}

	// GREATEST, а не простое присваивание: владелец может дать скидку
	// больше заработанной — ручное решение не должно стираться следующим
	// же заказом. И кабинет обещает именно рост, а не пересчёт в любую
	// сторону.
	if _, err := transaction.Exec(ctx, `
		UPDATE customers
		SET lifetime_spend_minor = $2,
			retail_discount_bps = GREATEST(retail_discount_bps, $3)
		WHERE id = $1
	`, customerID, spendMinor, DiscountBPS(spendMinor)); err != nil {
		return err
	}
	if _, err := transaction.Exec(ctx, `
		UPDATE orders SET spend_counted_at = CURRENT_TIMESTAMP
		WHERE customer_id = $1 AND status = 'completed' AND spend_counted_at IS NULL
	`, customerID); err != nil {
		return err
	}
	return transaction.Commit(ctx)
}
