package notify

import (
	"context"
	"fmt"
)

// Wording a customer sees on the lock screen. Statuses without an entry send
// nothing: "confirmed" or "assembling" are useful to a manager, not to the
// person waiting, and a shop that buzzes for every internal step gets muted.
var orderStatusWording = map[string]string{
	"ready":     "Заказ %s готов к выдаче",
	"shipped":   "Заказ %s передан в доставку",
	"completed": "Заказ %s получен. Спасибо!",
	"cancelled": "Заказ %s отменён",
	// Not a status of the order but news the customer is waiting on: the
	// delivery they were told a person would price has now been priced.
	"delivery_priced": "Доставка по заказу %s рассчитана — можно оплатить",
}

func (service *Service) NotifyOrderStatus(
	ctx context.Context,
	customerID int64,
	orderNumber, status string,
) error {
	if service == nil {
		return nil
	}
	wording, worth := orderStatusWording[status]
	if !worth {
		return nil
	}
	return service.SendToCustomer(ctx, customerID, Message{
		Title: "Фикусин",
		Body:  fmt.Sprintf(wording, orderNumber),
		URL:   "/account/orders/" + orderNumber,
		// One notification per order replaces the previous one instead of
		// stacking up.
		Tag: "order-" + orderNumber,
	})
}
