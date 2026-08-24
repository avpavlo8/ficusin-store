package migrate

import (
	"os"
	"strings"
	"testing"
)

func TestLegacyAICoverOptimizationMigrationUsesExistingPhotoQueue(t *testing.T) {
	raw, err := os.ReadFile("../../../timeweb/migrations/074_optimize_legacy_ai_covers.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	for _, required := range []string{
		"UPDATE product_media",
		"ai://catalog-cover/%",
		"mirror.card_url = mirror.large_url",
		"https://s3.twcstorage.ru/%",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration does not contain %q", required)
		}
	}
	for _, destructive := range []string{"DELETE FROM product_media", "TRUNCATE", "DROP TABLE"} {
		if strings.Contains(sql, destructive) {
			t.Fatalf("migration contains destructive operation %q", destructive)
		}
	}
}
