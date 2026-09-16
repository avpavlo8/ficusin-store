package httpapi

import (
	"errors"
	"strings"
	"testing"
)

func TestProcurementImportFailureMessageShowsSafeStage(t *testing.T) {
	tests := []struct {
		err      error
		contains string
	}{
		{errors.New("extract pdf text: pdftotext: exit status 1: broken xref"), "извлечь текст"},
		{errors.New("insert procurement document: ERROR: column revision_no does not exist"), "сохранить сам документ"},
		{errors.New("upsert procurement supplier alias: ERROR: duplicate key value violates unique constraint"), "названия товара поставщика"},
		{errors.New("reconcile procurement document line: ERROR: check constraint"), "сверку одной из строк"},
		{errors.New("commit procurement document import: connection reset"), "зафиксировать импорт"},
		{errors.New("unexpected internal failure: secret=value"), "внутреннем этапе"},
	}
	for _, test := range tests {
		message := procurementImportFailureMessage(test.err)
		if !strings.Contains(message, test.contains) {
			t.Fatalf("message %q does not contain %q", message, test.contains)
		}
		if strings.Contains(message, "secret=value") || strings.Contains(message, "column revision_no") || strings.Contains(message, "duplicate key") {
			t.Fatalf("message leaks internal error: %q", message)
		}
	}
}
