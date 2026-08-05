package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTelegramClientUsesEnvironmentToken(t *testing.T) {
	t.Parallel()

	var received struct {
		ChatID string `json:"chat_id"`
		Text   string `json:"text"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/botsecret-token/sendMessage" {
			t.Errorf("request path = %q, want %q", request.URL.Path, "/botsecret-token/sendMessage")
		}
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Errorf("decode request: %v", err)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client, err := NewTelegramClient(" -5430918511 ", " secret-token ")
	if err != nil {
		t.Fatalf("NewTelegramClient() error = %v", err)
	}
	client.apiBaseURL = server.URL
	client.httpClient = server.Client()

	err = client.SendOrder(context.Background(), TelegramOrder{
		OrderNumber:    "ZR-TEST",
		DeliveryMethod: "pickup",
		Items: []TelegramOrderItem{{
			Name: "Фикус", Price: 1000, Quantity: 1,
		}},
		Subtotal: 1000,
		Total:    1000,
	})
	if err != nil {
		t.Fatalf("SendOrder() error = %v", err)
	}
	if received.ChatID != "-5430918511" {
		t.Fatalf("chat_id = %q, want %q", received.ChatID, "-5430918511")
	}
	if received.Text == "" {
		t.Fatal("Telegram message is empty")
	}
}

// The message must never carry personal data: Telegram is a foreign
// service, and sending contacts there would be a cross-border transfer.
func TestTelegramMessageOmitsPersonalData(t *testing.T) {
	t.Parallel()

	var received struct {
		Text string `json:"text"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Errorf("decode request: %v", err)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client, err := NewTelegramClient("-5430918511", "secret-token")
	if err != nil {
		t.Fatalf("NewTelegramClient() error = %v", err)
	}
	client.apiBaseURL = server.URL
	client.httpClient = server.Client()

	if err := client.SendOrder(context.Background(), TelegramOrder{
		OrderNumber:    "ZR-TEST",
		DeliveryMethod: "cdek",
		DeliveryCity:   "Рязань",
		Items:          []TelegramOrderItem{{Name: "Фикус", Price: 1000, Quantity: 1}},
		Subtotal:       1000,
		Total:          1200,
	}); err != nil {
		t.Fatalf("SendOrder() error = %v", err)
	}

	for _, forbidden := range []string{"Покупатель:", "Телефон", "Email", "Адрес:"} {
		if strings.Contains(received.Text, forbidden) {
			t.Fatalf("Telegram message contains %q:\n%s", forbidden, received.Text)
		}
	}
	if !strings.Contains(received.Text, "ZR-TEST") {
		t.Fatalf("Telegram message lost the order number:\n%s", received.Text)
	}
}

func TestTelegramClientRejectsMissingCredentials(t *testing.T) {
	t.Parallel()

	if _, err := NewTelegramClient("-5430918511", ""); err == nil {
		t.Fatal("NewTelegramClient() error = nil, want missing token error")
	}
}

func TestTelegramClientRejectsMissingChatID(t *testing.T) {
	t.Parallel()

	if _, err := NewTelegramClient("", "secret-token"); err == nil {
		t.Fatal("NewTelegramClient() error = nil, want missing chat ID error")
	}
}
