package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// PaymentLinkNotice сообщает, куда ушла новая ссылка на оплату.
type PaymentLinkNotice struct {
	// Email: queued — письмо в очереди, no_email — у заказа нет почты,
	// disabled — почта магазина не настроена.
	Email string `json:"email"`
	// Push: sent — уведомление ушло (или у покупателя нет подписок, тогда
	// ничего не отправляется), guest — заказ без личного кабинета.
	Push string `json:"push"`
}

// NotifyNewPaymentLink кладёт письмо с новой ссылкой в общую очередь писем
// и отправляет покупателю пуш. Ссылку на оплату в пуше не передаём: она
// ведёт в заказ в личном кабинете, где кнопка «Оплатить» откроет ту же
// актуальную ссылку. Так пуш не устаревает при следующей замене ссылки.
func (repository *PostgresRepository) NotifyNewPaymentLink(ctx context.Context, orderID int64, paymentURL string) (PaymentLinkNotice, error) {
	notice := PaymentLinkNotice{Email: "no_email", Push: "guest"}
	var number, email string
	var customerID *int64
	var total float64
	err := repository.pool.QueryRow(ctx, `SELECT order_number, COALESCE(email,''), customer_id, total::DOUBLE PRECISION FROM orders WHERE id=$1`, orderID).Scan(&number, &email, &customerID, &total)
	if errors.Is(err, pgx.ErrNoRows) {
		return notice, errors.New("заказ не найден")
	}
	if err != nil {
		return notice, fmt.Errorf("load order for payment link: %w", err)
	}
	email = strings.TrimSpace(email)
	switch {
	case email == "":
	case !repository.packingMailConfigured:
		notice.Email = "disabled"
	default:
		subject := "Новая ссылка на оплату заказа " + number
		body := "Здравствуйте!\n\nМы подготовили новую ссылку на оплату заказа " + number + ".\n" +
			"Прежняя ссылка больше не действует.\n\nОплатить заказ: " + paymentURL + "\n\n" +
			"Если вы уже оплатили заказ по прежней ссылке, ничего делать не нужно: деньги не спишутся, " +
			"блокировка на карте снимется автоматически.\n\nФикусин"
		if _, err := repository.pool.Exec(ctx, `INSERT INTO outbox (recipient, subject, body) VALUES ($1, $2, $3)`, email, subject, body); err != nil {
			return notice, fmt.Errorf("queue payment link letter: %w", err)
		}
		notice.Email = "queued"
	}
	if customerID != nil && repository.notifier != nil {
		// Ошибка пуша не отменяет новую ссылку: письмо и кнопка в кабинете
		// уже работают, а сотрудник видит итог в ответе.
		if err := repository.notifier.NotifyOrderStatus(ctx, *customerID, number, "payment_link"); err != nil {
			notice.Push = "failed"
		} else {
			notice.Push = "sent"
		}
	}
	return notice, nil
}
