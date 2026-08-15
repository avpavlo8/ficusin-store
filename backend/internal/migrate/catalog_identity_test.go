package migrate

import (
	"os"
	"strings"
	"testing"
)

// The catalogue migration is deliberately additive: these assertions guard
// the compatibility contract that is easy to break during later cleanup.
func TestCatalogIdentityMigrationKeepsCompatibilityContract(t *testing.T) {
	raw, err := os.ReadFile("../../../timeweb/migrations/044_catalog_identity_attributes.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	for _, required := range []string{
		"'ficusin', 'legacy_sku'",
		"prevent_ficusin_sku_change",
		"product_external_ids",
		"product_attribute_values",
		"audience IN ('customer','technical')",
		"sync_marketplace_external_ids",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("catalog identity migration lost required contract %q", required)
		}
	}
	for _, destructive := range []string{"DROP TABLE products", "DROP COLUMN saby_id", "DROP COLUMN slug"} {
		if strings.Contains(sql, destructive) {
			t.Errorf("unsafe compatibility break found: %s", destructive)
		}
	}
}
