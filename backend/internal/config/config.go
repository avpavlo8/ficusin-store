package config

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	OrderRealization       OrderRealization
	FileStorageMode        string
	LocalPrivateFilesDir   string
	StaticAssetsBaseURL    string
	CatalogMarketplaces    Marketplaces
	CatalogPublicationMode string
	Environment            string
	StagingAccessToken     string
	HTTP                   HTTP
	Database               Database
	Auth                   Auth
	SMS                    SMS
	MigrationsDir          string
	TelegramChatID         string
	TelegramBotToken       string
	YandexSuggestKey       string
	MetrikaMPToken         string
	DaDataAPIKey           string
	AdminEmails            []string
	CDEK                   CDEK
	Push                   Push
	Payments               Payments
	SiteURL                string
	Mail                   Mail
	PrivatePhotos          Photos
	Photos                 Photos
	Marketplaces           Marketplaces
	Saby                   Saby
}

// Marketplaces holds seller API credentials. Empty credentials keep the
// corresponding channel disabled; secrets never live in the database.
type Marketplaces struct {
	WBToken           string
	OzonClientID      string
	OzonAPIKey        string
	AvitoClientID     string
	AvitoClientSecret string
	AvitoAddress      string
	AvitoManagerName  string
	AvitoContactPhone string
	AvitoCategory     string
	AvitoGoodsType    string
}

// Saby holds service authorization and the retail scope used by the
// procurement backend. Credentials stay in the process environment; only
// non-secret numeric identifiers are exposed by diagnostics.
type Saby struct {
	AppClientID string
	AppSecret   string
	SecretKey   string
	PointID     int64
	PriceListID int64
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
	// Zero keeps the production pool limit. Preview shares cluster capacity.
	MaxConnections int32
}

type Auth struct {
	CookieSecure bool
	SessionDays  int
}

// SMS configures SMS.ru's user-initiated call check, not an OTP delivery call.
// Without APIKey the development adapter fabricates a confirmed check. A
// production deployment must supply SMSRU_API_KEY for real phone verification.
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

	cfg := Config{
		HTTP: HTTP{
			Address:   defaultString(os.Getenv("HTTP_ADDRESS"), ":"+port),
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
		MigrationsDir:    strings.TrimSpace(os.Getenv("MIGRATIONS_DIR")),
		TelegramChatID:   defaultString(os.Getenv("TELEGRAM_ORDER_CHAT_ID"), "-5430918511"),
		TelegramBotToken: strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")),
		YandexSuggestKey: strings.TrimSpace(os.Getenv("YANDEX_SUGGEST_API_KEY")),
		MetrikaMPToken:   strings.TrimSpace(os.Getenv("METRIKA_MP_TOKEN")),
		DaDataAPIKey:     strings.TrimSpace(os.Getenv("DADATA_API_KEY")),
		AdminEmails:      splitList(os.Getenv("ADMIN_EMAILS")),
		CDEK: CDEK{
			ClientID:     strings.TrimSpace(os.Getenv("CDEK_CLIENT_ID")),
			ClientSecret: strings.TrimSpace(os.Getenv("CDEK_CLIENT_SECRET")),
		},
		SiteURL: defaultString(os.Getenv("SITE_URL"), "https://ficusin.ru"),
		Mail: Mail{
			Host:     strings.TrimSpace(os.Getenv("SMTP_HOST")),
			Port:     intFromEnv("SMTP_PORT", 465),
			Username: strings.TrimSpace(os.Getenv("SMTP_USERNAME")),
			Password: os.Getenv("SMTP_PASSWORD"),
			From:     strings.TrimSpace(os.Getenv("MAIL_FROM")),
			FromName: defaultString(os.Getenv("MAIL_FROM_NAME"), "Фикусин"),
		},
		Payments: Payments{
			ShopID:    strings.TrimSpace(os.Getenv("YOOKASSA_SHOP_ID")),
			SecretKey: strings.TrimSpace(os.Getenv("YOOKASSA_SECRET_KEY")),
			TaxSystem: intFromEnv("YOOKASSA_TAX_SYSTEM", 0),
			VATCode:   intFromEnv("YOOKASSA_VAT_CODE", 1),
		},
		PrivatePhotos: Photos{
			Endpoint:  defaultString(os.Getenv("PRIVATE_S3_ENDPOINT"), defaultString(os.Getenv("S3_ENDPOINT"), "https://s3.twcstorage.ru")),
			Region:    defaultString(os.Getenv("PRIVATE_S3_REGION"), defaultString(os.Getenv("S3_REGION"), "ru-1")),
			Bucket:    strings.TrimSpace(os.Getenv("PRIVATE_S3_BUCKET")),
			AccessKey: defaultString(os.Getenv("PRIVATE_S3_ACCESS_KEY"), os.Getenv("S3_ACCESS_KEY")),
			SecretKey: defaultString(os.Getenv("PRIVATE_S3_SECRET_KEY"), os.Getenv("S3_SECRET_KEY")),
		},
		Photos: Photos{
			Endpoint:  defaultString(os.Getenv("S3_ENDPOINT"), "https://s3.twcstorage.ru"),
			Region:    defaultString(os.Getenv("S3_REGION"), "ru-1"),
			Bucket:    strings.TrimSpace(os.Getenv("S3_BUCKET")),
			AccessKey: strings.TrimSpace(os.Getenv("S3_ACCESS_KEY")),
			SecretKey: strings.TrimSpace(os.Getenv("S3_SECRET_KEY")),
		},
		CatalogMarketplaces: Marketplaces{
			WBToken:      defaultString(os.Getenv("CATALOG_WB_API_TOKEN"), os.Getenv("WB_API_TOKEN")),
			OzonClientID: defaultString(os.Getenv("CATALOG_OZON_CLIENT_ID"), os.Getenv("OZON_CLIENT_ID")),
			OzonAPIKey:   defaultString(os.Getenv("CATALOG_OZON_API_KEY"), os.Getenv("OZON_API_KEY")),
		},
		Marketplaces: Marketplaces{
			WBToken:           strings.TrimSpace(os.Getenv("WB_API_TOKEN")),
			OzonClientID:      strings.TrimSpace(os.Getenv("OZON_CLIENT_ID")),
			OzonAPIKey:        strings.TrimSpace(os.Getenv("OZON_API_KEY")),
			AvitoClientID:     strings.TrimSpace(os.Getenv("AVITO_CLIENT_ID")),
			AvitoClientSecret: strings.TrimSpace(os.Getenv("AVITO_CLIENT_SECRET")),
			AvitoAddress:      defaultString(os.Getenv("AVITO_ADDRESS"), "Рязань, Новоселов 40а"),
			AvitoManagerName:  defaultString(os.Getenv("AVITO_MANAGER_NAME"), "Павловский Александр Владимирович"),
			AvitoContactPhone: defaultString(os.Getenv("AVITO_CONTACT_PHONE"), "+7 (915) 615-11-00"),
			AvitoCategory:     defaultString(os.Getenv("AVITO_CATEGORY"), "Растения"),
			AvitoGoodsType:    defaultString(os.Getenv("AVITO_GOODS_TYPE"), "Комнатные растения"),
		},
		Saby: Saby{
			AppClientID: strings.TrimSpace(os.Getenv("SABY_APP_CLIENT_ID")),
			AppSecret:   strings.TrimSpace(os.Getenv("SABY_APP_SECRET")),
			SecretKey:   strings.TrimSpace(os.Getenv("SABY_SECRET_KEY")),
			PointID:     int64FromEnv("SABY_POINT_ID", 278),
			PriceListID: int64FromEnv("SABY_PRICE_LIST_ID", 6),
		},
		Push: Push{
			PublicKey:  strings.TrimSpace(os.Getenv("VAPID_PUBLIC_KEY")),
			PrivateKey: strings.TrimSpace(os.Getenv("VAPID_PRIVATE_KEY")),
			Subject:    defaultString(os.Getenv("VAPID_SUBJECT"), "mailto:info@ficusin.ru"),
		},
	}
	if cfg.PrivatePhotos.Bucket != "" && cfg.PrivatePhotos.Bucket == cfg.Photos.Bucket && strings.TrimRight(cfg.PrivatePhotos.Endpoint, "/") == strings.TrimRight(cfg.Photos.Endpoint, "/") {
		return Config{}, errors.New("PRIVATE_S3_BUCKET must differ from the public S3_BUCKET")
	}
	cfg.StaticAssetsBaseURL = strings.TrimRight(strings.TrimSpace(os.Getenv("STATIC_ASSETS_BASE_URL")), "/")
	if cfg.StaticAssetsBaseURL != "" {
		u, e := url.Parse(cfg.StaticAssetsBaseURL)
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return Config{}, errors.New("STATIC_ASSETS_BASE_URL must be an HTTPS bucket/release URL without credentials or query")
		}
	}
	cfg.CatalogPublicationMode = defaultString(os.Getenv("CATALOG_PUBLICATION_MODE"), "disabled")
	if cfg.CatalogPublicationMode != "disabled" && cfg.CatalogPublicationMode != "mock" && cfg.CatalogPublicationMode != "live" {
		return Config{}, errors.New("CATALOG_PUBLICATION_MODE must be disabled, mock or live")
	}
	configured, err := configureEnvironment(cfg)
	if err != nil {
		return Config{}, err
	}
	if configured.CatalogPublicationMode == "live" && (configured.Environment != "production" || os.Getenv("CATALOG_PUBLICATION_LIVE_ENABLED") != "true") {
		return Config{}, errors.New("live catalog publication requires production and CATALOG_PUBLICATION_LIVE_ENABLED=true")
	}
	configured.FileStorageMode = defaultString(os.Getenv("FILE_STORAGE_MODE"), "s3")
	if configured.FileStorageMode != "s3" && configured.FileStorageMode != "mock" {
		return Config{}, errors.New("FILE_STORAGE_MODE must be s3 or mock")
	}
	if configured.FileStorageMode == "mock" && configured.Environment != "staging" {
		return Config{}, errors.New("FILE_STORAGE_MODE=mock is allowed only in isolated APP_ENV=staging")
	}
	configured.LocalPrivateFilesDir = strings.TrimSpace(os.Getenv("LOCAL_PRIVATE_FILES_DIR"))
	if configured.LocalPrivateFilesDir != "" {
		if configured.Environment != "development" || configured.CatalogPublicationMode != "mock" {
			return Config{}, errors.New("LOCAL_PRIVATE_FILES_DIR is allowed only with APP_ENV=development and CATALOG_PUBLICATION_MODE=mock")
		}
		if !filepath.IsAbs(configured.LocalPrivateFilesDir) || filepath.Clean(configured.LocalPrivateFilesDir) == string(filepath.Separator) {
			return Config{}, errors.New("LOCAL_PRIVATE_FILES_DIR must be a dedicated absolute directory")
		}
	}
	if err = configureOrderRealization(&configured); err != nil {
		return Config{}, err
	}
	return configured, nil
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

func int64FromEnv(name string, fallback int64) int64 {
	value, err := strconv.ParseInt(strings.TrimSpace(os.Getenv(name)), 10, 64)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
