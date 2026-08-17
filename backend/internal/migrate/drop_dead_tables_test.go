package migrate

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// DROP необратим, и миграции применяются автоматически при старте. Эта
// проверка стережёт ровно две таблицы: если в файл когда-нибудь допишут
// третью — тест упадёт до выкладки, а не после потери данных.
func TestDropMigrationRemovesOnlyDeadTables(t *testing.T) {
	raw, err := os.ReadFile("../../../timeweb/migrations/052_drop_dead_tables.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)

	allowed := map[string]bool{
		"integration_credentials": true,
		"otp_codes":               true,
	}
	dropped := regexp.MustCompile(`(?i)DROP TABLE (?:IF EXISTS )?([a-z_]+)`).FindAllStringSubmatch(sql, -1)
	if len(dropped) != len(allowed) {
		t.Fatalf("миграция удаляет %d таблиц, ожидали %d", len(dropped), len(allowed))
	}
	for _, match := range dropped {
		if !allowed[match[1]] {
			t.Errorf("миграция удаляет неожиданную таблицу %s", match[1])
		}
	}

	// CASCADE унесёт вместе с таблицей всё, что на неё ссылается, и сделает
	// это молча. Лучше падение миграции.
	if strings.Contains(strings.ToUpper(sql), "CASCADE") {
		t.Error("CASCADE удалит зависимости молча — пусть миграция лучше упадёт")
	}

	// В той же базе живёт Ficusin Content Bot. Его таблицы — чужие.
	for _, foreign := range []string{"content_bot_state", "content_publication_claims", "schema_migrations"} {
		if strings.Contains(sql, foreign) && strings.Contains(sql, "DROP TABLE IF EXISTS "+foreign) {
			t.Errorf("миграция магазина трогает чужую таблицу %s", foreign)
		}
	}
}
