package order

import (
	"os"
	"strings"
	"testing"
)

// Резерв и количество в заказе — разные числа. Покупатель просит пять,
// свободно три: резервируем три, в quantity лежит пять. Возврат по quantity
// забирал две штуки из чужих резервов, а если свободного не было вовсе —
// возвращал все пять, ничего до этого не заняв. Так одно растение можно
// продать дважды, и заметно это только на складе.
func TestReleaseStockGivesBackReservedNotOrdered(t *testing.T) {
	raw, err := os.ReadFile("stock.go")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)

	if !strings.Contains(sql, "SUM(reserved_qty)") {
		t.Error("возврат резерва обязан считать по reserved_qty")
	}
	if strings.Contains(sql, "SUM(quantity)") {
		t.Error("возврат резерва снова считает по quantity — это возврат чужого товара")
	}
	// Блокировка строк склада и защита от повторного возврата — часть того
	// же инварианта: без них две отмены вернут товар дважды.
	for _, required := range []string{"FOR UPDATE", "stock_released_at"} {
		if !strings.Contains(sql, required) {
			t.Errorf("возврат резерва потерял %q", required)
		}
	}
}

// Заказ «после подтверждения менеджером» лежит в том же payment_status
// pending, что и брошенная оплата картой. Общее условие автоотмены
// отменяло его вместе с резервом, пока менеджер до него не дошёл.
func TestExpiryCancelsOnlyAbandonedOnlinePayments(t *testing.T) {
	raw, err := os.ReadFile("expire.go")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)

	if !strings.Contains(sql, "payment_method = 'online'") {
		t.Error("автоотмена снова ловит все pending-заказы, включая ожидающие менеджера")
	}
	if !strings.Contains(sql, "status NOT IN ('cancelled', 'completed')") {
		t.Error("автоотмена потеряла защиту от повторной отмены")
	}
}

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
