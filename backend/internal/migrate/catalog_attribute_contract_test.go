package migrate

import (
	"os"
	"strings"
	"testing"
)

func TestCatalogAttributeContractMigrationIsNonDestructive(t *testing.T) {
	raw, err := os.ReadFile("../../../timeweb/migrations/077_catalog_attribute_contract_stage1.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ToUpper(string(raw))
	for _, forbidden := range []string{
		"DELETE FROM PRODUCT_ATTRIBUTE_VALUES",
		"DELETE FROM VARIANT_ATTRIBUTE_VALUES",
		"DROP TABLE",
		"DROP COLUMN",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("stage 1 migration contains destructive operation %q", forbidden)
		}
	}
	for _, required := range []string{"ON CONFLICT(ATTRIBUTE_ID,CODE) DO UPDATE", "SET IS_REQUIRED=FALSE", "FILTER.CODE IN ('LIGHT','WATERING','CARE','POT','PETS')"} {
		if !strings.Contains(sql, required) {
			t.Errorf("stage 1 migration lost safety contract %q", required)
		}
	}
}
