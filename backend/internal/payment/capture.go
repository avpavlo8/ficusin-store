package payment

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/jackc/pgx/v5"
)

type capturer interface {
	CapturePayment(ctx context.Context, paymentID, idempotenceKey string) error
}

type paymentOperationTx struct {
	pgx.Tx
	releaseOnce sync.Once
	release     func()
}

func (tx *paymentOperationTx) Commit(ctx context.Context) error {
	defer tx.releaseOnce.Do(tx.release)
	return tx.Tx.Commit(ctx)
}

func (tx *paymentOperationTx) Rollback(ctx context.Context) error {
	defer tx.releaseOnce.Do(tx.release)
	return tx.Tx.Rollback(ctx)
}

// Payment operations share one transaction-scoped advisory lock. Order edits
// retire their old payment before changing the order, so a capture and an edit
// cannot choose opposite actions for the same attempt concurrently.
func (service *Service) lockPaymentOperation(ctx context.Context, paymentRowID int64) (pgx.Tx, error) {
	select {
	case service.operationSlots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	release := func() { <-service.operationSlots }
	tx, err := service.pool.Begin(ctx)
	if err != nil {
		release()
		return nil, err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, -paymentRowID); err != nil {
		_ = tx.Rollback(ctx)
		release()
		return nil, err
	}
	return &paymentOperationTx{Tx: tx, release: release}, nil
}

// resolveAuthorization runs before an authorized payment is recorded locally.
// The provider operation is idempotent, so a timeout leaves the row pending
// and the next webhook or reconciliation pass can safely fetch/retry it.
func (service *Service) resolveAuthorization(ctx context.Context, payment integration.Payment, paymentRowID, orderID int64, offerID *int64, expected float64, localStatus string) error {
	if payment.Status != "waiting_for_capture" {
		return nil
	}
	var invalid error
	if cents(payment.Amount) != cents(expected) {
		invalid = errors.New("сумма авторизации не совпадает с заказом")
	} else if localStatus != StatusPending {
		invalid = errors.New("платёжная ссылка больше не действует")
	} else if offerID != nil {
		invalid = service.checkAuthorizedOffer(ctx, *offerID, orderID, expected)
	} else {
		invalid = service.checkAuthorizedOrder(ctx, orderID, expected)
	}
	if invalid != nil {
		closable, ok := service.provider.(canceller)
		if !ok {
			return fmt.Errorf("авторизация требует отмены: %w: %v", ErrCancelUnsupported, invalid)
		}
		if err := closable.CancelPayment(ctx, payment.ID, fmt.Sprintf("cancel-%d", paymentRowID)); err != nil {
			return fmt.Errorf("отменить удержание после проверки заказа: %w", err)
		}
		if _, err := service.pool.Exec(ctx, `UPDATE payments SET last_error=$2,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, paymentRowID, invalid.Error()); err != nil {
			return err
		}
		if localStatus == StatusPending {
			if offerID != nil {
				if _, err := service.pool.Exec(ctx, `UPDATE shipment_offers SET status='stale',updated_at=CURRENT_TIMESTAMP WHERE id=$1 AND status='payment_pending'`, *offerID); err != nil {
					return err
				}
			} else if _, err := service.pool.Exec(ctx, `UPDATE orders SET payment_method=$2 WHERE id=$1 AND payment_method=$3 AND status NOT IN ('cancelled','completed')`, orderID, MethodManager, MethodOnline); err != nil {
				return err
			}
		}
		service.logger.Warn("удержание ЮKassa отменено после повторной проверки", "payment_id", payment.ID, "reason", invalid)
		return nil
	}
	capture, ok := service.provider.(capturer)
	if !ok {
		return errors.New("провайдер не поддерживает подтверждение удержания")
	}
	if err := capture.CapturePayment(ctx, payment.ID, fmt.Sprintf("capture-%d", paymentRowID)); err != nil {
		return fmt.Errorf("подтвердить удержание ЮKassa: %w", err)
	}
	return nil
}

func (service *Service) checkAuthorizedOrder(ctx context.Context, orderID int64, expected float64) error {
	state, err := service.moneyStateByOrderID(ctx, orderID)
	if err != nil {
		return err
	}
	balance := balanceFromState(state)
	if state.status == "cancelled" || state.status == "completed" || state.method != MethodOnline || !balance.Ready || cents(balance.Due) != cents(expected) {
		return errors.New("заказ или сумма оплаты изменились")
	}
	return service.checkOrderStock(ctx, orderID)
}

func (service *Service) checkAuthorizedOffer(ctx context.Context, offerID, orderID int64, expected float64) error {
	var status, orderStatus, address, addressSnapshot string
	var amount float64
	var expires time.Time
	var offerRevision, revision int
	err := service.pool.QueryRow(ctx, `SELECT so.status,o.status,o.address,so.address_snapshot,
		(CASE WHEN so.delivery_payee='carrier' THEN so.subtotal ELSE so.total END)::DOUBLE PRECISION,
		so.expires_at,so.order_revision,o.shipment_revision
		FROM shipment_offers so JOIN orders o ON o.id=so.order_id
		WHERE so.id=$1 AND o.id=$2`, offerID, orderID).Scan(&status, &orderStatus, &address, &addressSnapshot, &amount, &expires, &offerRevision, &revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("предложение отправки больше не найдено")
	}
	if err != nil {
		return err
	}
	if status != "payment_pending" || orderStatus == "cancelled" || orderStatus == "completed" || !expires.After(time.Now()) || address != addressSnapshot || offerRevision != revision || cents(amount) != cents(expected) {
		return errors.New("предложение отправки или его сумма изменились")
	}
	return service.checkOfferStock(ctx, offerID)
}
