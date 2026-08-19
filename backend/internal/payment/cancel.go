package payment

import (
	"context"
	"errors"
	"fmt"
)

// canceller — возможность провайдера закрыть неоплаченный платёж.
//
// Отдельным интерфейсом, а не строкой в provider: провайдер, который не умеет
// отменять, остаётся рабочим провайдером, а тестовые заглушки не приходится
// переписывать ради метода, который им не нужен.
type canceller interface {
	CancelPayment(ctx context.Context, paymentID, idempotenceKey string) error
}

// ErrCancelUnsupported — провайдер не умеет отменять платежи.
var ErrCancelUnsupported = errors.New("провайдер не умеет отменять платежи")

// CancelPending закрывает незавершённые платежи заказа там, где они живут —
// у провайдера, а не только в нашей таблице.
//
// Вызывается до открытия транзакции отмены: сетевой запрос под блокировками
// склада держал бы чужие оформления. Если провайдер отказал — значит
// платёж жив или уже оплачен, и заказ отменять нельзя.
func (service *Service) CancelPending(ctx context.Context, orderID int64) error {
	// Магазин без ключей ЮKassa картой не торгует — гасить нечего.
	if !service.Configured() {
		return nil
	}
	closable, ok := service.provider.(canceller)
	if !ok {
		return ErrCancelUnsupported
	}

	rows, err := service.pool.Query(ctx, `
		SELECT id, COALESCE(provider_payment_id, '')
		FROM payments
		WHERE order_id = $1 AND status = $2
		ORDER BY id
	`, orderID, StatusPending)
	if err != nil {
		return fmt.Errorf("load pending payments: %w", err)
	}
	type attempt struct {
		id         int64
		providerID string
	}
	attempts := make([]attempt, 0, 2)
	for rows.Next() {
		var current attempt
		if err := rows.Scan(&current.id, &current.providerID); err != nil {
			rows.Close()
			return fmt.Errorf("scan pending payment: %w", err)
		}
		attempts = append(attempts, current)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read pending payments: %w", err)
	}

	for _, current := range attempts {
		// Платёж без номера у провайдера туда так и не доехал: гасить его
		// негде, достаточно закрыть у себя.
		if current.providerID == "" {
			continue
		}
		key, err := idempotenceKey()
		if err != nil {
			return err
		}
		if err := closable.CancelPayment(ctx, current.providerID, key); err != nil {
			return fmt.Errorf("платёж %s не отменён: %w", current.providerID, err)
		}
		if _, err := service.pool.Exec(ctx, `
			UPDATE payments SET status = $2, updated_at = CURRENT_TIMESTAMP
			WHERE id = $1
		`, current.id, StatusCancelled); err != nil {
			return fmt.Errorf("mark payment cancelled: %w", err)
		}
	}
	return nil
}
