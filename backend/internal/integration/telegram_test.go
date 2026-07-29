package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

	client, err := NewTelegramClient(nil, " -5430918511 ", " secret-token ")
	if err != nil {
		t.Fatalf("NewTelegramClient() error = %v", err)
	}
	client.apiBaseURL = server.URL
	client.httpClient = server.Client()

	err = client.SendOrder(context.Background(), TelegramOrder{
		OrderNumber:    "ZR-TEST",
		CustomerName:   "Покупатель",
		Phone:          "+79156151100",
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

func TestTelegramClientRejectsMissingCredentials(t *testing.T) {
	t.Parallel()

	if _, err := NewTelegramClient(nil, "-5430918511", ""); err == nil {
		t.Fatal("NewTelegramClient() error = nil, want missing token error")
	}
}

func TestTelegramClientRejectsMissingChatID(t *testing.T) {
	t.Parallel()

	if _, err := NewTelegramClient(nil, "", "secret-token"); err == nil {
		t.Fatal("NewTelegramClient() error = nil, want missing chat ID error")
	}
}
