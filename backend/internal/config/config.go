package config

import (
	"errors"
	"os"
	"strings"
)

type Config struct {
	HTTP                  HTTP
	Database              Database
	Auth                  Auth
	MigrationsDir         string
	IntegrationPrivateKey string
	TelegramChatID        string
	AdminEmails           []string
}

type HTTP struct {
	Address   string
	StaticDir string
}

type Database struct {
	URL       string
	CA        string
	VerifyTLS bool
}

type Auth struct {
	CookieSecure bool
	SessionDays  int
}

func Load() (Config, error) {
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8080"
	}

	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is not set")
	}

	return Config{
		HTTP: HTTP{
			Address:   ":" + port,
			StaticDir: strings.TrimSpace(os.Getenv("STATIC_DIR")),
		},
		Database: Database{
			URL:       databaseURL,
			CA:        normalizedPEM(os.Getenv("DATABASE_SSL_CA")),
			VerifyTLS: databaseTLSVerificationEnabled(os.Getenv("DATABASE_SSL_VERIFY")),
		},
		Auth: Auth{
			CookieSecure: booleanEnabled(os.Getenv("AUTH_COOKIE_SECURE"), true),
			SessionDays:  boundedInteger(os.Getenv("AUTH_SESSION_DAYS"), 30, 1, 90),
		},
		MigrationsDir:         strings.TrimSpace(os.Getenv("MIGRATIONS_DIR")),
		IntegrationPrivateKey: strings.TrimSpace(os.Getenv("INTEGRATION_SECRETS_PRIVATE_KEY")),
		TelegramChatID:        defaultString(os.Getenv("TELEGRAM_ORDER_CHAT_ID"), "-5430918511"),
		AdminEmails:           splitList(os.Getenv("ADMIN_EMAILS")),
	}, nil
}

func splitList(value string) []string {
	result := make([]string, 0)
	for _, item := range strings.Split(value, ",") {
		if item = strings.ToLower(strings.TrimSpace(item)); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func defaultString(value, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return fallback
}

func databaseTLSVerificationEnabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "false", "0":
		return false
	default:
		return true
	}
}

func normalizedPEM(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, `"'`)
	return strings.ReplaceAll(value, `\n`, "\n")
}

func booleanEnabled(value string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1":
		return true
	case "false", "0":
		return false
	default:
		return fallback
	}
}

func boundedInteger(value string, fallback, minimum, maximum int) int {
	parsed := 0
	for _, character := range strings.TrimSpace(value) {
		if character < '0' || character > '9' {
			return fallback
		}
		parsed = parsed*10 + int(character-'0')
	}
	if parsed < minimum {
		return fallback
	}
	if parsed > maximum {
		return maximum
	}
	return parsed
}
