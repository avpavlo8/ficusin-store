package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"
)

// TelegramOrder is deliberately free of personal data. Telegram is a
// foreign service, so sending a customer's name, phone, email or address
// there would be a cross-border transfer of personal data. The message is
// only a heads-up that an order arrived; the contacts live in the admin
// panel, where they never leave our own infrastructure.
type TelegramOrder struct {
	OrderNumber    string
	DeliveryMethod string
	// DeliveryCity is the city of a CDEK pick-up point. It says nothing
	// about a particular person, and the manager needs it to judge urgency.
	DeliveryCity string
	DeliveryFee  float64
	// DeliveryFeePending means the price is waiting for the manager: no box
	// dimensions, CDEK unavailable, or the customer asked to pack the plants
	// together. Total is short by the delivery until then.
	DeliveryFeePending bool
	// PaymentStatus and PaymentMethod are the first thing a manager needs to
	// know: an unpaid order must not be packed and sent.
	PaymentStatus string
	PaymentMethod string
	// RepackRequested is the customer asking whether the plants fit into one
	// box. It is the one thing in this message that needs an answer.
	RepackRequested bool
	Subtotal        float64
	Total        float64
	Items        []TelegramOrderItem
}

type TelegramOrderItem struct {
	Name     string
	Price    float64
	Quantity int
}

type TelegramClient struct {
	chatID     string
	botToken   string
	apiBaseURL string
	httpClient *http.Client
}

func NewTelegramClient(chatID, botToken string) (*TelegramClient, error) {
	chatID = strings.TrimSpace(chatID)
	botToken = strings.TrimSpace(botToken)
	if chatID == "" {
		return nil, errors.New("Telegram chat ID is not configured")
	}
	if botToken == "" {
		return nil, errors.New("Telegram bot token is not configured")
	}
	return &TelegramClient{
		chatID:     chatID,
		botToken:   botToken,
		apiBaseURL: "https://api.telegram.org",
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}, nil
}

func (client *TelegramClient) SendOrder(ctx context.Context, order TelegramOrder) error {
	message := orderMessage(order)
	body, err := json.Marshal(map[string]any{
		"chat_id":                  client.chatID,
		"text":                     message,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	})
	if err != nil {
		return err
	}
	return client.post(ctx, body)
}

// orderMessage is what the manager reads. Contacts never appear here: this
// goes to a foreign service, and the panel is where personal data stays.
func orderMessage(order TelegramOrder) string {
	deliveryLabels := map[string]string{
		"pickup":  "Самовывоз в Рязани",
		"courier": "Курьер по Рязани",
		"cdek":    "СДЭК по России",
		"post":    "Почта России",
	}
	money := func(value float64) string { return fmt.Sprintf("%.0f ₽", value) }
	lines := []string{"🌿 <b>Новый заказ " + html.EscapeString(order.OrderNumber) + "</b>", ""}
	for _, item := range order.Items {
		lines = append(lines, fmt.Sprintf(
			"• %s × %d — %s",
			html.EscapeString(item.Name),
			item.Quantity,
			html.EscapeString(money(item.Price*float64(item.Quantity))),
		))
	}
	receiving := deliveryLabels[order.DeliveryMethod]
	if order.DeliveryCity != "" {
		receiving += ", " + order.DeliveryCity
	}
	deliveryLine := money(order.DeliveryFee)
	totalLine := money(order.Total) + " без доставки"
	if !order.DeliveryFeePending {
		totalLine = money(order.Total)
	} else if order.RepackRequested {
		deliveryLine = "покупатель просит упаковать в одну коробку — рассчитайте и сообщите"
	} else {
		deliveryLine = "рассчитайте вручную и сообщите покупателю"
	}
	payment := "❗️ НЕ ОПЛАЧЕН — ждём оплату"
	switch order.PaymentStatus {
	case "paid":
		payment = "✅ Оплачен"
	case "on_delivery":
		payment = "💵 Оплата при получении"
	case "invoice":
		payment = "🧾 Ждёт счёт на организацию"
	case "cancelled":
		payment = "✖️ Оплата отменена"
	}
	lines = append(lines,
		"",
		"<b>Оплата:</b> "+html.EscapeString(payment),
		"<b>Товары:</b> "+html.EscapeString(money(order.Subtotal)),
		"<b>Доставка:</b> "+html.EscapeString(deliveryLine),
		"<b>Итого:</b> "+html.EscapeString(totalLine),
		"<b>Получение:</b> "+html.EscapeString(receiving),
		"",
		"Контакты покупателя — в панели управления.",
	)
	return truncateRunes(strings.Join(lines, "\n"), 4000)
}

func (client *TelegramClient) post(ctx context.Context, body []byte) error {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		strings.TrimRight(client.apiBaseURL, "/")+"/bot"+client.botToken+"/sendMessage",
		bytes.NewReader(body),
	)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("send Telegram order: %w", err)
	}
	defer response.Body.Close()
	var result struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || !result.OK {
		return fmt.Errorf("Telegram: %s", result.Description)
	}
	return nil
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
