package config

import "testing"

func TestStagingRejectsProductionDatabaseAndOrigin(t *testing.T) {
	t.Setenv("APP_ENV", "staging")
	t.Setenv("STAGING_ACCESS_TOKEN", "a-separate-test-secret-for-staging-only")
	t.Setenv("DATABASE_URL", "postgres://localhost/ficusin")
	t.Setenv("SITE_URL", "https://preview.twc1.net")
	if _, err := Load(); err == nil {
		t.Fatal("production database accepted")
	}
	t.Setenv("DATABASE_URL", "postgres://localhost/ficusin_staging_20261031")
	t.Setenv("SITE_URL", "https://ficusin.ru")
	if _, err := Load(); err == nil {
		t.Fatal("production origin accepted")
	}
}

func TestStagingDisablesCopiedProductionCredentials(t *testing.T) {
	t.Setenv("APP_ENV", "staging")
	t.Setenv("STAGING_ACCESS_TOKEN", "a-separate-test-secret-for-staging-only")
	t.Setenv("DATABASE_URL", "postgres://localhost/ficusin_staging_20261031")
	t.Setenv("SITE_URL", "https://preview.twc1.net")
	for _, key := range []string{"SABY_APP_CLIENT_ID", "SABY_APP_SECRET", "SABY_SECRET_KEY", "WB_API_TOKEN", "OZON_CLIENT_ID", "OZON_API_KEY", "YOOKASSA_SHOP_ID", "YOOKASSA_SECRET_KEY", "SMSRU_API_KEY", "TELEGRAM_BOT_TOKEN", "VAPID_PRIVATE_KEY", "VAPID_PUBLIC_KEY", "SMTP_HOST", "S3_ACCESS_KEY", "S3_SECRET_KEY", "METRIKA_MP_TOKEN", "AVITO_CLIENT_ID", "AVITO_CLIENT_SECRET"} {
		t.Setenv(key, "production-secret")
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Saby != (Saby{}) || cfg.Marketplaces != (Marketplaces{}) || cfg.Payments != (Payments{}) || cfg.SMS != (SMS{}) || cfg.Push != (Push{}) || cfg.Mail != (Mail{}) || cfg.CDEK != (CDEK{}) || cfg.TelegramBotToken != "" || cfg.MetrikaMPToken != "" || cfg.Photos.AccessKey != "" || cfg.Photos.SecretKey != "" {
		t.Fatal("external credentials survived staging isolation")
	}
	if cfg.ValidateDatabaseName("ficusin") == nil || cfg.ValidateDatabaseName("ficusin_staging_other") == nil {
		t.Fatal("connection redirect accepted")
	}
	if err := cfg.ValidateDatabaseName("ficusin_staging_20261031"); err != nil {
		t.Fatal(err)
	}
}
