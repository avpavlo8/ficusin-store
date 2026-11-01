package main

import (
	"context"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/config"
	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
)

func TestStagingStartsWithoutLiveTelegram(t *testing.T) {
	client, err := newTelegramNotifications(config.Config{Environment: "staging", TelegramChatID: "real-chat", TelegramBotToken: "copied-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := client.(disabledTelegramNotifications); !ok {
		t.Fatal("staging selected a network client")
	}
	if err := client.SendOrder(context.Background(), integration.TelegramOrder{}); err != nil {
		t.Fatal(err)
	}
	if err := client.SendOperations(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := newTelegramNotifications(config.Config{Environment: "production"}); err == nil {
		t.Fatal("production accepted missing Telegram credentials")
	}
	production, err := newTelegramNotifications(config.Config{Environment: "production", TelegramChatID: "chat", TelegramBotToken: "token"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := production.(*integration.TelegramClient); !ok {
		t.Fatal("production notifier changed")
	}
}
