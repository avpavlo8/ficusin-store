package main

import (
	"context"

	"github.com/avpavlo8/ficusin-store/backend/internal/config"
	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
)

type telegramNotifications interface {
	SendOrder(context.Context, integration.TelegramOrder) error
	SendOperations(context.Context, string) error
}

// A preview must boot without live Telegram credentials, and no worker or
// operations probe may accidentally send a test event to the real chat.
type disabledTelegramNotifications struct{}

func (disabledTelegramNotifications) SendOrder(context.Context, integration.TelegramOrder) error {
	return nil
}
func (disabledTelegramNotifications) SendOperations(context.Context, string) error { return nil }

func newTelegramNotifications(cfg config.Config) (telegramNotifications, error) {
	if cfg.Environment == "staging" {
		return disabledTelegramNotifications{}, nil
	}
	return integration.NewTelegramClient(cfg.TelegramChatID, cfg.TelegramBotToken)
}
