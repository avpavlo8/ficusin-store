package httpapi

import "strings"

// procurementImportFailureMessage converts internal import stages into a safe,
// actionable message. The full database/process error remains in the server log;
// the browser only receives the stage so an owner can report where the import
// stopped without exposing SQL, paths or credentials.
func procurementImportFailureMessage(err error) string {
	if err == nil {
		return "Не удалось импортировать PDF. Подробности записаны в журнал сервера"
	}
	message := err.Error()
	stages := []struct {
		prefix string
		text   string
	}{
		{"extract pdf text:", "Не удалось извлечь текст из PDF. Файл прочитан, но сервер не смог выполнить pdftotext"},
		{"begin import procurement document:", "PDF распознан, но не удалось начать запись закупки в базу"},
		{"load procurement supplier:", "PDF распознан, но не удалось загрузить поставщика из базы"},
		{"query duplicate procurement document:", "PDF распознан, но не удалось проверить документ на повторную загрузку"},
		{"create or attach procurement order:", "PDF распознан, но не удалось привязать его к закупке"},
		{"check procurement document conflict:", "PDF распознан, но не удалось проверить конфликт номера инвойса"},
		{"load active procurement document:", "PDF распознан, но не удалось проверить текущую ревизию инвойса"},
		{"insert procurement document:", "PDF распознан, но не удалось сохранить сам документ в базе"},
		{"archive procurement invoice lines:", "PDF распознан, но не удалось сохранить историю предыдущей ревизии"},
		{"supersede procurement document:", "PDF распознан, но не удалось закрыть предыдущую ревизию инвойса"},
		{"reset procurement plan comparison:", "PDF распознан, но не удалось подготовить план закупки к новой сверке"},
		{"archive added procurement lines:", "PDF распознан, но не удалось архивировать строки прошлой сверки"},
		{"upsert procurement supplier alias:", "PDF распознан, но ошибка возникла при сохранении названия товара поставщика"},
		{"refresh supplier product availability:", "PDF распознан, но ошибка возникла при обновлении наличия товара поставщика"},
		{"find procurement plan line:", "PDF распознан, но не удалось найти строку плана для сверки"},
		{"reconcile procurement document line:", "PDF распознан, но не удалось сохранить сверку одной из строк инвойса"},
		{"mark missing procurement plan lines:", "PDF распознан, но не удалось отметить отсутствующие в инвойсе позиции"},
		{"compare procurement plan with invoice:", "PDF распознан, но не удалось завершить сравнение плана с инвойсом"},
		{"update procurement document status:", "PDF распознан и сохранён в транзакции, но не удалось обновить статус документа"},
		{"update procurement order status:", "PDF распознан и сохранён в транзакции, но не удалось обновить статус закупки"},
		{"rebalance requests after invoice:", "PDF распознан, но не удалось перераспределить клиентские заявки после сверки"},
		{"commit procurement document import:", "PDF обработан, но база не смогла зафиксировать импорт"},
	}
	for _, stage := range stages {
		if strings.HasPrefix(message, stage.prefix) {
			return stage.text
		}
	}
	return "PDF распознан, но импорт остановился на внутреннем этапе. Подробности записаны в журнал сервера"
}
