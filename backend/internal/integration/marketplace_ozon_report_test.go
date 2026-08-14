package integration

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Плашка канала говорила «Ozon не вернул отправлений», и по этой строке
// нельзя было отличить пустой список от списка без доставленных и от
// доставленных без кода продавца. Тест держит подробности: без них каждый
// следующий заход начинается с догадки.
func TestOzonFailureIsExplainedByWhatWasSeen(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if strings.Contains(request.URL.Path, "/fbs/") {
			_, _ = response.Write([]byte(`{"result":{"postings":[` +
				`{"created_at":"2026-08-01T10:00:00Z","status":"cancelled","products":[{"offer_id":"OZ-1","quantity":1,"price":"100"}]},` +
				`{"created_at":"2026-08-01T11:00:00Z","status":"cancelled","products":[]}]}}`))
			return
		}
		_, _ = response.Write([]byte(`{"result":[` +
			`{"created_at":"2026-08-01T12:00:00Z","status":"delivered","products":[{"offer_id":"","quantity":2,"price":"200"}]}]}`))
	}))
	defer server.Close()

	executor := NewMarketplaceExecutor("", "client", "secret")
	executor.ozonBase, executor.client = server.URL, server.Client()
	day := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	cause := errors.New("Ozon не вернул отправлений")
	err := executor.DescribeSalesFailure(context.Background(), "ozon", day, day, cause)
	if !errors.Is(err, cause) {
		t.Fatalf("исходная причина потеряна: %v", err)
	}
	for _, fragment := range []string{
		"/v3/posting/fbs/list", "cancelled 2", "доставленных 0",
		"/v2/posting/fbo/list", "доставленных 1", "с кодом продавца 0",
	} {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("в сообщении нет «%s»: %s", fragment, err.Error())
		}
	}
}

// Чужой канал разведка не трогает: там ей сказать нечего, а лишний заход к
// площадке стоит лимита.
func TestDescribeSalesFailureLeavesOtherChannelsAlone(t *testing.T) {
	executor := NewMarketplaceExecutor("token", "client", "secret")
	cause := errors.New("Wildberries ответил 429")
	day := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	if err := executor.DescribeSalesFailure(context.Background(), "wb", day, day, cause); err != cause {
		t.Fatalf("ошибка канала wb изменена: %v", err)
	}
}
