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
		SELECT id, COALESCE(provider_payment_id, ''),
			COALESCE(request_payload->'metadata'->>'ficusin_capture_mode','')
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
		mode       string
	}
	attempts := make([]attempt, 0, 2)
	for rows.Next() {
		var current attempt
		if err := rows.Scan(&current.id, &current.providerID, &current.mode); err != nil {
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
		// No provider id after a timeout does not prove that the request failed:
		// YooKassa may have accepted it and lost only our response. Cancelling the
		// order here could release stock while the buyer is paying.
		if current.providerID == "" {
			return ErrPaymentNeedsReview
		}
		payment, err := service.provider.FetchPayment(ctx, current.providerID)
		if err != nil {
			return err
		}
		if payment.Status == "pending" && current.mode == "two_stage" {
			// Pending cannot be canceled at YooKassa. Retire the local link;
			// the reconciliation worker will cancel it if authorization arrives.
			if _, err := service.pool.Exec(ctx, `UPDATE payments SET status=$2,updated_at=CURRENT_TIMESTAMP WHERE id=$1 AND status=$3`, current.id, StatusSuperseded, StatusPending); err != nil {
				return err
			}
			continue
		}
		if payment.Status == "pending" || payment.Status == "succeeded" {
			return ErrPaymentNeedsReview
		}
		if payment.Status != "canceled" && payment.Status != "cancelled" && payment.Status != "waiting_for_capture" {
			return fmt.Errorf("неизвестное состояние платежа ЮKassa: %s", payment.Status)
		}
		if payment.Status == "waiting_for_capture" {
			if err := closable.CancelPayment(ctx, current.providerID, fmt.Sprintf("cancel-%d", current.id)); err != nil {
				return fmt.Errorf("платёж %s не отменён: %w", current.providerID, err)
			}
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
