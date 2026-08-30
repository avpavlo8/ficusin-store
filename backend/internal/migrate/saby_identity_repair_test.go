package migrate

import (
	"os"
	"strings"
	"testing"
)

func TestSabyIdentityRepairIsConservative(t *testing.T) {
	raw, err := os.ReadFile("../../../timeweb/migrations/049_repair_legacy_saby_identity.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	for _, required := range []string{
		"old.missing_since IS NOT NULL",
		"HAVING COUNT(*) = 1",
		"old.saby_id <> current.saby_id",
		"NOT EXISTS (\n    SELECT 1 FROM products other",
		"NOT EXISTS (\n    SELECT 1 FROM product_variants other",
		"available_qty = EXCLUDED.available_qty",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("Saby identity repair lost safety condition %q", required)
		}
	}
	for _, forbidden := range []string{
		"DELETE FROM products",
		"DELETE FROM product_variants",
		"DELETE FROM saby_nomenclature",
		"UPDATE products product\nSET slug",
		"UPDATE products product\nSET name",
		"UPDATE products product\nSET description",
	} {
		if strings.Contains(sql, forbidden) {
			t.Errorf("Saby identity repair became destructive: %q", forbidden)
		}
	}
}

func TestProcurementSabyIdentityRepairKeepsOneCurrentCard(t *testing.T) {
	raw, err := os.ReadFile("../../../timeweb/migrations/081_procurement_canonical_saby_ids.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	for _, required := range []string{
		"saby_id ~ '^[0-9]+$'",
		"HAVING COUNT(*) = 1",
		"UPDATE procurement_supplier_aliases",
		"UPDATE procurement_order_lines",
		"INSERT INTO procurement_product_channels",
		"item.status = 'completed'",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("procurement Saby repair lost safety condition %q", required)
		}
	}
	for _, forbidden := range []string{
		"DELETE FROM saby_nomenclature",
		"DELETE FROM procurement_orders",
		"DELETE FROM procurement_order_lines",
		"status = 'received'",
	} {
		if strings.Contains(sql, forbidden) {
			t.Errorf("procurement Saby repair became destructive: %q", forbidden)
		}
	}
}
