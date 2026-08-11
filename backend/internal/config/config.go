package config

import (
		"errors"
		"os"
		"strconv"
	"strings"
	)

type Config struct {
		HTTP                  HTTP
		Database              Database
		Auth                  Auth
		SMS                   SMS
		MigrationsDir         string
		TelegramChatID        string
		TelegramBotToken      string
		YandexSuggestKey      string
		AdminEmails           []string
		CDEK                  CDEK
		Push                  Push
		Payments              Payments
		SiteURL               string
		Mail                  Mail
	Photos                 Photos
	Marketplaces           Marketplaces
}

// Marketplaces holds seller API credentials. Empty credentials keep the
// corresponding channel disabled; secrets never live in the database.
type Marketplaces struct {
	WBToken      string
	OzonClientID string
	OzonAPIKey   string
}

// Mail is the SMTP account the shop writes from. An empty host switches
// letters off, like every other integration here.
type Mail struct {
		Host     string
		Port     int
		Username string
		Password string
		From     string
		FromName string
}

// Photos is the S3-compatible bucket where the shop keeps its own copies of
// product pictures. Empty keys switch the mirror off: the shop then keeps
// showing the supplier's links — slower and borrowed, but working.
type Photos struct {
		Endpoint  string
		Region    string
		Bucket    string
		AccessKey string
		SecretKey string
}

// Payments holds the YooKassa credentials. Empty values switch card
// payment off: the checkout then offers only the ways that need no
// provider, and nothing else breaks.
type Payments struct {
		ShopID    string
		SecretKey string
		// SendReceipt is on only when YooKassa prints the fiscal receipt.
		// A shop that punches receipts through its own till leaves this off,
		// otherwise one sale produces two receipts.
		SendReceipt bool
		// TaxSystem and VATCode are the codes from 54-ФЗ. They matter only
		// when we are the ones sending the receipt.
		TaxSystem int
		VATCode   int
}

// CDEK holds the API credentials for the delivery service. Empty values
// switch pick-up points off; the other delivery methods keep working.
type CDEK struct {
		ClientID     string
		ClientSecret string
}

// Push holds the VAPID key pair that identifies this shop to the browsers'
// push services. Empty keys simply turn notifications off; nothing else
// breaks.
type Push struct {
		PublicKey  string
		PrivateKey string
		// Subject is a mailto: or https: address a push service can use to
		// reach us if our notifications cause trouble.
		Subject string
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

// SMS holds settings for the SMS.ru gateway used to deliver OTP login
// codes over a call (no sender name registration required). If APIKey is
// empty, codes are only logged (useful for local development) and no call
// is actually placed.
type SMS struct {
		APIKey string
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
					SMS: SMS{
									APIKey: strings.TrimSpace(os.Getenv("SMSRU_API_KEY")),
								},
					MigrationsDir:         strings.TrimSpace(os.Getenv("MIGRATIONS_DIR")),
					TelegramChatID:        defaultString(os.Getenv("TELEGRAM_ORDER_CHAT_ID"), "-5430918511"),
					TelegramBotToken:      strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")),
					YandexSuggestKey:      strings.TrimSpace(os.Getenv("YANDEX_SUGGEST_API_KEY")),
					AdminEmails:           splitList(os.Getenv("ADMIN_EMAILS")),
					CDEK: CDEK{
									ClientID:     strings.TrimSpace(os.Getenv("CDEK_CLIENT_ID")),
									ClientSecret: strings.TrimSpace(os.Getenv("CDEK_CLIENT_SECRET")),
								},
					SiteURL:               defaultString(os.Getenv("SITE_URL"), "https://ficusin.ru"),
					Mail: Mail{
									Host:     strings.TrimSpace(os.Getenv("SMTP_HOST")),
									Port:     intFromEnv("SMTP_PORT", 465),
									Username: strings.TrimSpace(os.Getenv("SMTP_USERNAME")),
									Password: os.Getenv("SMTP_PASSWORD"),
									From:     strings.TrimSpace(os.Getenv("MAIL_FROM")),
									FromName: defaultString(os.Getenv("MAIL_FROM_NAME"), "Фикусин"),
								},
					Payments: Payments{
									ShopID:      strings.TrimSpace(os.Getenv("YOOKASSA_SHOP_ID")),
									SecretKey:   strings.TrimSpace(os.Getenv("YOOKASSA_SECRET_KEY")),
									SendReceipt: strings.TrimSpace(os.Getenv("YOOKASSA_SEND_RECEIPT")) == "1",
									TaxSystem:   intFromEnv("YOOKASSA_TAX_SYSTEM", 0),
									VATCode:     intFromEnv("YOOKASSA_VAT_CODE", 1),
								},
					Photos: Photos{
									Endpoint:  defaultString(os.Getenv("S3_ENDPOINT"), "https://s3.twcstorage.ru"),
									Region:    defaultString(os.Getenv("S3_REGION"), "ru-1"),
									Bucket:    strings.TrimSpace(os.Getenv("S3_BUCKET")),
									AccessKey: strings.TrimSpace(os.Getenv("S3_ACCESS_KEY")),
									SecretKey: strings.TrimSpace(os.Getenv("S3_SECRET_KEY")),
								},
					Marketplaces: Marketplaces{
									WBToken:      strings.TrimSpace(os.Getenv("WB_API_TOKEN")),
									OzonClientID: strings.TrimSpace(os.Getenv("OZON_CLIENT_ID")),
									OzonAPIKey:   strings.TrimSpace(os.Getenv("OZON_API_KEY")),
								},
					Push: Push{
									PublicKey:  strings.TrimSpace(os.Getenv("VAPID_PUBLIC_KEY")),
									PrivateKey: strings.TrimSpace(os.Getenv("VAPID_PRIVATE_KEY")),
									Subject:    defaultString(os.Getenv("VAPID_SUBJECT"), "mailto:info@ficusin.ru"),
								},
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

// intFromEnv reads a numeric setting, falling back rather than failing:
// a typo in an optional code should not stop the shop from starting.
func intFromEnv(name string, fallback int) int {
		value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
		if err != nil {
				return fallback
		}
		return value
}
