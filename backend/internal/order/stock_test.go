package order

import (
	"os"
	"strings"
	"testing"
)

// Миграция обязана заполнить reserved_qty у уже лежащих заказов, иначе
// первая же отмена старого заказа не вернёт ничего.
func TestReservedQuantityMigrationBackfillsExistingOrders(t *testing.T) {
	raw, err := os.ReadFile("../../../timeweb/migrations/051_order_reserved_quantity.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)

	for _, required := range []string{
		"ADD COLUMN IF NOT EXISTS reserved_qty",
		"UPDATE order_items",
		"CASE WHEN is_preorder = 0 THEN quantity ELSE 0 END",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("миграция потеряла %q", required)
		}
	}
	for _, destructive := range []string{"DROP TABLE", "DROP COLUMN", "DELETE FROM"} {
		if strings.Contains(strings.ToUpper(sql), destructive) {
			t.Errorf("миграция не должна содержать %s", destructive)
		}
	}
}
