package migrate

import (
	"os"
	"strings"
	"testing"
)

func TestCatalogFinalMigrationRepairsHistoricalOrdersBeforeEnforcingIdentity(t *testing.T) {
	raw, err := os.ReadFile("../../../timeweb/migrations/056_catalog_pim_final.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)

	repair := strings.Index(sql, "Some very old order lines predate variant_id")
	guard := strings.Index(sql, "catalog v2 cannot migrate order_items without variant identity")
	if repair < 0 || guard < 0 || repair >= guard {
		t.Fatal("historical order repair must run before the final identity guard")
	}
	for _, required := range []string{
		"WHERE variant_id IS NULL",
		"nextval('ficusin_product_code_seq')",
		"nextval('ficusin_sku_seq')",
		"'Исторический вариант'",
		"is_active",
		"SET variant_id = placeholder_variant_id",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("catalog migration lost orphan-order repair marker %q", required)
		}
	}
}
