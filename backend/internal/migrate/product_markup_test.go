package migrate

import (
	"os"
	"strings"
	"testing"
)

// Описания приехали из СБИС с редакторской разметкой, и на карточке товара
// покупатель читал теги как текст. Чистка в синхронизации эти строки уже не
// трогает: описание принадлежит магазину. Проверка стережёт саму миграцию —
// она обязана оставаться идемпотентной и не трогать ничего, кроме текстовых
// полей карточки.
func TestProductMarkupMigrationOnlyRewritesDirtyText(t *testing.T) {
	raw, err := os.ReadFile("../../../timeweb/migrations/050_clean_product_markup.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)

	for _, required := range []string{
		"ficusin_plain_markup",
		"<[^>]*>",
		"SET description = ficusin_plain_markup(description)",
		"SET short_description = ficusin_plain_markup(short_description)",
		"SET care_instructions = ficusin_plain_markup(care_instructions)",
		// Без WHERE запрос переписал бы все 245 карточек, включая чистые.
		"WHERE description ~ ",
		"DROP FUNCTION IF EXISTS ficusin_plain_markup",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("миграция потеряла обязательную часть %q", required)
		}
	}

	for _, destructive := range []string{
		"DROP TABLE",
		"DROP COLUMN",
		"DELETE FROM",
		"TRUNCATE",
	} {
		if strings.Contains(strings.ToUpper(sql), destructive) {
			t.Errorf("миграция чистки текста не должна содержать %s", destructive)
		}
	}

	// Единственные колонки, которые ей позволено трогать.
	for _, line := range strings.Split(sql, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "SET ") {
			continue
		}
		if !strings.HasPrefix(trimmed, "SET description") &&
			!strings.HasPrefix(trimmed, "SET short_description") &&
			!strings.HasPrefix(trimmed, "SET care_instructions") &&
			!strings.HasPrefix(trimmed, "SET latin_name") {
			t.Errorf("миграция меняет неожиданную колонку: %s", trimmed)
		}
	}
}
