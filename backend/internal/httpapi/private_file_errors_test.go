package httpapi

import (
	"fmt"
	"github.com/avpavlo8/ficusin-store/backend/internal/fileassets"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUnavailablePrivateFileStorageIsClear503(t *testing.T) {
	w := httptest.NewRecorder()
	if !privateFileUnavailable(w, fmt.Errorf("context: %w", fileassets.ErrUnavailable)) || w.Code != 503 || !strings.Contains(w.Body.String(), "Файл не сохранён") {
		t.Fatal(w.Code, w.Body.String())
	}
}
