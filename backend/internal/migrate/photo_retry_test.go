package migrate

import (
	"os"
	"strings"
	"testing"
)

func TestPhotoRetryMigrationOnlyRequeuesUnmirroredSabyMedia(t *testing.T) {
	raw, err := os.ReadFile("../../../timeweb/migrations/045_retry_external_photo_migration.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	for _, required := range []string{"mirror.card_url IS NULL", "https://disk.sbis.ru/%", "product_media", "attempts = 0"} {
		if !strings.Contains(sql, required) {
			t.Errorf("photo retry migration lost safety condition %q", required)
		}
	}
	for _, destructive := range []string{"DELETE FROM product_media", "UPDATE product_media", "TRUNCATE"} {
		if strings.Contains(sql, destructive) {
			t.Errorf("photo retry migration must not change product references: %s", destructive)
		}
	}
}
