package config

import (
	"errors"
	"net/url"
	"os"
	"regexp"
	"strings"
)

var stagingDatabase = regexp.MustCompile(`^ficusin_staging(?:_[a-z0-9_]+)?$`)

func configureEnvironment(cfg Config) (Config, error) {
	cfg.Environment = defaultString(os.Getenv("APP_ENV"), "production")
	if cfg.Environment != "production" && cfg.Environment != "staging" && cfg.Environment != "development" {
		return Config{}, errors.New("APP_ENV must be production, staging or development")
	}
	if cfg.Environment != "staging" {
		return cfg, nil
	}
	cfg.StagingAccessToken = strings.TrimSpace(os.Getenv("STAGING_ACCESS_TOKEN"))
	cfg.Database.MaxConnections = 3
	if len(cfg.StagingAccessToken) < 32 {
		return Config{}, errors.New("staging requires a separate STAGING_ACCESS_TOKEN with at least 32 characters")
	}
	db, err := url.Parse(cfg.Database.URL)
	if err != nil || !stagingDatabase.MatchString(strings.TrimPrefix(db.Path, "/")) {
		return Config{}, errors.New("staging requires an isolated database named ficusin_staging or ficusin_staging_*")
	}
	site, err := url.Parse(cfg.SiteURL)
	if err != nil || site.Scheme != "https" || site.Hostname() == "" || site.User != nil || site.Hostname() == "ficusin.ru" || site.Hostname() == "www.ficusin.ru" {
		return Config{}, errors.New("staging requires its own HTTPS SITE_URL, separate from ficusin.ru")
	}
	// Even accidental copying of production environment variables must not
	// enable real external writes in a preview. Integrations are exercised in
	// isolated, explicitly guarded regression workflows instead.
	cfg.SMS = SMS{}
	cfg.TelegramBotToken, cfg.TelegramChatID = "", ""
	cfg.YandexSuggestKey, cfg.DaDataAPIKey, cfg.MetrikaMPToken = "", "", ""
	cfg.CDEK, cfg.Payments, cfg.Mail, cfg.Push = CDEK{}, Payments{}, Mail{}, Push{}
	cfg.Photos.AccessKey, cfg.Photos.SecretKey = "", ""
	cfg.PrivatePhotos.AccessKey, cfg.PrivatePhotos.SecretKey = "", ""
	cfg.Marketplaces, cfg.Saby = Marketplaces{}, Saby{}
	return cfg, nil
}

// ValidateDatabaseName is called after connecting and before ANY migration or
// worker starts. A proxy/connection option must not redirect staging to prod.
func (cfg Config) ValidateDatabaseName(actual string) error {
	if cfg.Environment != "staging" {
		return nil
	}
	db, err := url.Parse(cfg.Database.URL)
	if err != nil || !stagingDatabase.MatchString(actual) || actual != strings.TrimPrefix(db.Path, "/") {
		return errors.New("refusing to start staging on a different or production database")
	}
	return nil
}
