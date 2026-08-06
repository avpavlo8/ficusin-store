package mail

import (
	"fmt"
	"strings"
)

// OrderLetter is everything the letters need to know about an order.
type OrderLetter struct {
	Number        string
	CustomerName  string
	Items         []OrderLine
	Subtotal      float64
	DeliveryFee   float64
	FeePending    bool
	Total         float64
	Delivery      string
	Address       string
	PaymentStatus string
	PaymentMethod string
	PayURL        string
	TrackNumber   string
}

type OrderLine struct {
	Name     string
	Price    float64
	Quantity int
}

var deliveryWording = map[string]string{
	"pickup":  "Самовывоз, Рязань, Новосёлов, 40А",
	"courier": "Курьер по Рязани",
	"cdek":    "СДЭК до пункта выдачи",
	"post":    "Почта России",
}

// Confirmation is the letter sent the moment an order is placed. It repeats
// what the person just bought — the one thing they will look for later when
// they wonder whether the order went through at all.
func Confirmation(order OrderLetter) Letter {
	lines := []string{
		greeting(order.CustomerName),
		"",
		"Мы получили ваш заказ " + order.Number + ".",
		"",
		"Что в заказе:",
	}
	lines = append(lines, itemLines(order)...)
	lines = append(lines, "", totals(order)...)
	if receiving := deliveryWording[order.Delivery]; receiving != "" {
		lines = append(lines, "", "Получение: "+receiving)
		if order.Address != "" && order.Delivery != "pickup" {
			lines = append(lines, order.Address)
		}
	}

	switch {
	case order.PaymentStatus == "paid":
		lines = append(lines, "", "Заказ оплачен. Мы соберём его и сообщим, когда он поедет.")
	case order.PaymentMethod == "on_delivery":
		lines = append(lines, "", "Оплата при получении — заплатите, когда заберёте заказ.")
	case order.PaymentMethod == "invoice":
		lines = append(lines, "", "Счёт на организацию пришлём отдельным письмом.")
	case order.FeePending:
		lines = append(lines,
			"",
			"Стоимость доставки рассчитает менеджер: как только она будет известна,",
			"мы пришлём ссылку на оплату, и заказ появится в личном кабинете.",
		)
	case order.PayURL != "":
		lines = append(lines, "", "Оплатить заказ: "+order.PayURL)
	}

	return Letter{
		To:      "",
		Subject: "Заказ " + order.Number + " принят",
		Body:    strings.Join(append(lines, "", signature()), "\n"),
	}
}

// StatusChange tells the customer the one thing that changed. Statuses with
// no wording send nothing: internal steps are the manager's business.
func StatusChange(order OrderLetter, status string) (Letter, bool) {
	wording := map[string]string{
		"ready":     "Заказ %s готов к выдаче",
		"shipped":   "Заказ %s передан в доставку",
		"completed": "Заказ %s получен. Спасибо, что выбрали нас!",
		"cancelled": "Заказ %s отменён",
	}[status]
	if wording == "" {
		return Letter{}, false
	}
	subject := fmt.Sprintf(wording, order.Number)
	lines := []string{greeting(order.CustomerName), "", subject + "."}

	switch status {
	case "shipped":
		if order.TrackNumber != "" {
			lines = append(lines,
				"",
				"Трек-номер СДЭК: "+order.TrackNumber,
				"Отследить: https://www.cdek.ru/ru/tracking?order_id="+order.TrackNumber,
			)
		}
	case "ready":
		if order.Delivery == "pickup" {
			lines = append(lines, "", "Забрать: "+deliveryWording["pickup"])
		} else if order.Address != "" {
			lines = append(lines, "", "Пункт выдачи: "+order.Address)
		}
	case "cancelled":
		lines = append(lines, "", "Если это ошибка — ответьте на это письмо, разберёмся.")
	}

	return Letter{
		Subject: subject,
		Body:    strings.Join(append(lines, "", signature()), "\n"),
	}, true
}

// Invoice is for a wholesale customer who pays from a company account.
func Invoice(order OrderLetter, requisites string) Letter {
	lines := []string{
		greeting(order.CustomerName),
		"",
		"Счёт на оплату заказа " + order.Number + ".",
		"",
		"Что в заказе:",
	}
	lines = append(lines, itemLines(order)...)
	lines = append(lines, "", totals(order)...)
	if requisites != "" {
		lines = append(lines, "", "Реквизиты для оплаты:", requisites)
	}
	lines = append(lines,
		"",
		"В назначении платежа укажите номер заказа "+order.Number+".",
		"Заказ отправим после поступления оплаты.",
	)
	return Letter{
		Subject: "Счёт по заказу " + order.Number,
		Body:    strings.Join(append(lines, "", signature()), "\n"),
	}
}

func itemLines(order OrderLetter) []string {
	lines := make([]string, 0, len(order.Items))
	for _, item := range order.Items {
		lines = append(lines, fmt.Sprintf(
			"• %s × %d — %s",
			item.Name, item.Quantity, money(item.Price*float64(item.Quantity)),
		))
	}
	return lines
}

func totals(order OrderLetter) []string {
	delivery := money(order.DeliveryFee)
	total := money(order.Total)
	if order.FeePending {
		delivery = "рассчитает менеджер"
		total = money(order.Total) + " + доставка"
	}
	return []string{
		"Товары: " + money(order.Subtotal),
		"Доставка: " + delivery,
		"Итого: " + total,
	}
}

func greeting(name string) string {
	name = strings.TrimSpace(strings.Split(strings.TrimSpace(name), " ")[0])
	if name == "" {
		return "Здравствуйте!"
	}
	return "Здравствуйте, " + name + "!"
}

func signature() string {
	return strings.Join([]string{
		"—",
		"Фикусин, комнатные растения",
		"ficusin.ru",
	}, "\n")
}

func money(value float64) string {
	return fmt.Sprintf("%.0f ₽", value)
}
