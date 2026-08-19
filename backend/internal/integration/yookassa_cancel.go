package integration

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

// CancelPayment закрывает неоплаченный платёж на стороне ЮKassa.
//
// Отменяя заказ у себя, магазин раньше гасил платёж только в своей таблице,
// а в ЮKassa он оставался живым. Покупатель мог вернуться по старой ссылке
// и заплатить за заказ, которого больше нет: товар уже вернулся на полку,
// а деньги приходили.
//
// ЮKassa позволяет отменить только платёж, который ещё не оплачен. Если
// покупатель успел заплатить, в ответ придёт ошибка — и это правильный
// ответ: деньги уже взяты, дальше нужен возврат, а не отмена.
//
// Лежит отдельно от yookassa.go нарочно: добавление одного метода не должно
// переписывать файл, который работает с деньгами.
func (client *YooKassaClient) CancelPayment(
	ctx context.Context,
	paymentID string,
	idempotenceKey string,
) error {
	if !client.Configured() {
		return errors.New("оплата не настроена")
	}
	paymentID = strings.TrimSpace(paymentID)
	if paymentID == "" {
		return errors.New("не указан платёж")
	}
	var response yooKassaPayment
	if err := client.send(
		ctx,
		http.MethodPost,
		"/payments/"+paymentID+"/cancel",
		idempotenceKey,
		map[string]any{},
		&response,
	); err != nil {
		return err
	}
	// ЮKassa отвечает статусом платежа. Любой ответ, кроме canceled, значит,
	// что платёж жив — и заказ отменять нельзя.
	if response.Status != "" && response.Status != "canceled" {
		return errors.New("ЮKassa не отменила платёж: " + response.Status)
	}
	return nil
}
