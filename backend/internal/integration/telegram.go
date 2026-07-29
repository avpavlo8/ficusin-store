package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"
)

type TelegramCredentials struct {
	BotToken string `json:"botToken"`
}

type TelegramOrder struct {
	OrderNumber    string
	CustomerName   string
	Phone          string
	Email          string
	Address        string
	Comment        string
	DeliveryMethod string
	DeliveryFee    float64
	Subtotal       float64
	Total          float64
	Items          []TelegramOrderItem
}

type TelegramOrderItem struct {
	Name     string
	Price    float64
	Quantity int
}

type TelegramClient struct {
	credentials *CredentialStore
	chatID      string
	httpClient  *http.Client
}

func NewTelegramClient(credentials *CredentialStore, chatID string) *TelegramClient {
	return &TelegramClient{
		credentials: credentials,
		chatID:      chatID,
		httpClient:  &http.Client{Timeout: 15 * time.Second},
	}
}

func (client *TelegramClient) SendOrder(ctx context.Context, order TelegramOrder) error {
	credentials, err := GetCredentials[TelegramCredentials](ctx, client.credentials, "telegram")
	if err != nil {
		return err
	}
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
	lines = append(lines,
		"",
		"<b>Товары:</b> "+html.EscapeString(money(order.Subtotal)),
		"<b>Доставка:</b> "+html.EscapeString(money(order.DeliveryFee)),
		"<b>Итого:</b> "+html.EscapeString(money(order.Total)),
		"<b>Получение:</b> "+html.EscapeString(deliveryLabels[order.DeliveryMethod]),
	)
	if order.Address != "" {
		lines = append(lines, "<b>Адрес:</b> "+html.EscapeString(order.Address))
	}
	lines = append(lines,
		"",
		"<b>Покупатель:</b> "+html.EscapeString(order.CustomerName),
		"<b>Телефон:</b> "+html.EscapeString(order.Phone),
		"<b>Email:</b> "+html.EscapeString(order.Email),
	)
	if order.Comment != "" {
		lines = append(lines, "<b>Комментарий:</b> "+html.EscapeString(truncateRunes(order.Comment, 500)))
	}
	message := truncateRunes(strings.Join(lines, "\n"), 4000)
	body, err := json.Marshal(map[string]any{
		"chat_id":                  client.chatID,
		"text":                     message,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"https://api.telegram.org/bot"+strings.TrimSpace(credentials.BotToken)+"/sendMessage",
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
