package config

import (
	"strings"
	"testing"
)

func TestDatabaseTLSVerificationEnabled(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		"":        true,
		"true":    true,
		"FALSE":   false,
		" false ": false,
		"0":       false,
	}

	for value, expected := range tests {
		if actual := databaseTLSVerificationEnabled(value); actual != expected {
			t.Errorf("databaseTLSVerificationEnabled(%q) = %v, want %v", value, actual, expected)
		}
	}
}

func TestNormalizedPEM(t *testing.T) {
	t.Parallel()

	input := `"-----BEGIN CERTIFICATE-----\nvalue\n-----END CERTIFICATE-----"`
	expected := "-----BEGIN CERTIFICATE-----\nvalue\n-----END CERTIFICATE-----"
	if actual := normalizedPEM(input); actual != expected {
		t.Fatalf("normalizedPEM() = %q, want %q", actual, expected)
	}
}

func TestBoundedInteger(t *testing.T) {
	t.Parallel()

	if actual := boundedInteger("120", 30, 1, 90); actual != 90 {
		t.Fatalf("boundedInteger() = %d, want 90", actual)
	}
	if actual := boundedInteger("invalid", 30, 1, 90); actual != 30 {
		t.Fatalf("boundedInteger() = %d, want 30", actual)
	}
}

func TestLoadReadsTelegramEnvironment(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgresql://localhost/ficusin")
	t.Setenv("TELEGRAM_BOT_TOKEN", " secret-token ")
	t.Setenv("TELEGRAM_ORDER_CHAT_ID", " -5430918511 ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.TelegramBotToken != "secret-token" {
		t.Fatalf("TelegramBotToken = %q, want %q", cfg.TelegramBotToken, "secret-token")
	}
	if cfg.TelegramChatID != "-5430918511" {
		t.Fatalf("TelegramChatID = %q, want %q", cfg.TelegramChatID, "-5430918511")
	}
}

func TestCatalogPublicationRequiresExplicitLiveOptIn(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgresql://localhost/ficusin")
	t.Setenv("APP_ENV", "development")
	t.Setenv("CATALOG_PUBLICATION_MODE", "mock")
	cfg, err := Load()
	if err != nil || cfg.CatalogPublicationMode != "mock" {
		t.Fatalf("mock unavailable: %v", err)
	}
	t.Setenv("CATALOG_PUBLICATION_MODE", "live")
	if _, err = Load(); err == nil {
		t.Fatal("development allowed real writes")
	}
	t.Setenv("APP_ENV", "production")
	t.Setenv("CATALOG_PUBLICATION_LIVE_ENABLED", "")
	if _, err = Load(); err == nil {
		t.Fatal("production allowed live writes without second opt-in")
	}
	t.Setenv("CATALOG_PUBLICATION_LIVE_ENABLED", "true")
	if _, err = Load(); err != nil {
		t.Fatal(err)
	}
}

func TestPrivateStorageDoesNotInheritPublicBucket(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/local")
	t.Setenv("APP_ENV", "development")
	t.Setenv("S3_BUCKET", "public-photos")
	t.Setenv("S3_ACCESS_KEY", "public-key")
	t.Setenv("PRIVATE_S3_BUCKET", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PrivatePhotos.Bucket != "" {
		t.Fatal("public bucket was silently selected for private uploads")
	}
	t.Setenv("PRIVATE_S3_BUCKET", "public-photos")
	if _, err = Load(); err == nil {
		t.Fatal("public/private bucket collision accepted")
	}
	t.Setenv("PRIVATE_S3_BUCKET", "private-files")
	cfg, err = Load()
	if err != nil || cfg.PrivatePhotos.Bucket != "private-files" {
		t.Fatal(err)
	}
	t.Setenv("STATIC_ASSETS_BASE_URL", "https://s3.example/bucket/release")
	cfg, err = Load()
	if err != nil || cfg.StaticAssetsBaseURL == "" {
		t.Fatal(err)
	}
	t.Setenv("STATIC_ASSETS_BASE_URL", "https://user:secret@s3.example/bucket")
	if _, err = Load(); err == nil {
		t.Fatal("asset URL accepted embedded credentials")
	}
}

func TestLocalPrivateFilesRequireExplicitDevelopmentMock(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/ficusin_local_test")
	t.Setenv("LOCAL_PRIVATE_FILES_DIR", t.TempDir())
	t.Setenv("PRIVATE_S3_BUCKET", "")
	t.Setenv("CATALOG_PUBLICATION_MODE", "mock")
	t.Setenv("APP_ENV", "development")
	cfg, err := Load()
	if err != nil || cfg.LocalPrivateFilesDir == "" {
		t.Fatal("development mock rejected", err)
	}
	t.Setenv("STAGING_ACCESS_TOKEN", strings.Repeat("local-test-", 4))
	t.Setenv("SITE_URL", "https://isolated-staging.example.test")
	t.Setenv("DATABASE_URL", "postgres://localhost/ficusin_staging_private_test")
	for _, environment := range []string{"staging", "production"} {
		t.Setenv("APP_ENV", environment)
		if _, err = Load(); err == nil || !strings.Contains(err.Error(), "LOCAL_PRIVATE_FILES_DIR") {
			t.Fatal("local private files not rejected explicitly outside development", environment, err)
		}
	}
	t.Setenv("APP_ENV", "development")
	t.Setenv("CATALOG_PUBLICATION_MODE", "disabled")
	if _, err = Load(); err == nil {
		t.Fatal("local files allowed without explicit mock mode")
	}
	t.Setenv("CATALOG_PUBLICATION_MODE", "mock")
	t.Setenv("LOCAL_PRIVATE_FILES_DIR", "relative")
	if _, err = Load(); err == nil {
		t.Fatal("relative local directory accepted")
	}
	t.Setenv("LOCAL_PRIVATE_FILES_DIR", "")
	t.Setenv("CATALOG_PUBLICATION_MODE", "disabled")
	cfg, err = Load()
	if err != nil || cfg.LocalPrivateFilesDir != "" {
		t.Fatal("default gained a silent filesystem fallback", err)
	}
}

func TestExplicitStagingFileStorageMock(t *testing.T) {
	t.Setenv("APP_ENV", "staging")
	t.Setenv("STAGING_ACCESS_TOKEN", strings.Repeat("staging-test-", 4))
	t.Setenv("DATABASE_URL", "postgres://localhost/ficusin_staging_file_mock_test")
	t.Setenv("SITE_URL", "https://isolated-staging.example.test")
	t.Setenv("LOCAL_PRIVATE_FILES_DIR", "")
	t.Setenv("PRIVATE_S3_BUCKET", "")
	t.Setenv("FILE_STORAGE_MODE", "mock")
	cfg, err := Load()
	if err != nil || cfg.FileStorageMode != "mock" {
		t.Fatal("explicit staging mock rejected", err)
	}
	for _, environment := range []string{"production", "development"} {
		t.Setenv("APP_ENV", environment)
		if _, err = Load(); err == nil || !strings.Contains(err.Error(), "FILE_STORAGE_MODE") {
			t.Fatal("mock allowed outside staging", environment, err)
		}
	}
	t.Setenv("FILE_STORAGE_MODE", "")
	cfg, err = Load()
	if err != nil || cfg.FileStorageMode != "s3" {
		t.Fatal("implicit mock fallback appeared", err)
	}
}
