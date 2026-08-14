package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Ozon отдаёт отправления окном, а не всей историей: годовой запрос
// возвращается пустым, и со стороны это неотличимо от «продаж не было».
// Тест держит разбивку по месяцам и отсутствие статуса в фильтре — у FBS и
// FBO наборы значений разные, и неизвестное площадке значение молча
// возвращает пустоту.
func TestOzonPostingsAreRequestedByMonthlyWindows(t *testing.T) {
	windows := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Path != "/v3/posting/fbs/list" {
			_, _ = response.Write([]byte(`{"result":{"postings":[]}}`))
			return
		}
		var payload struct {
			Filter map[string]any `json:"filter"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("разобрать запрос: %v", err)
		}
		if _, hasStatus := payload.Filter["status"]; hasStatus {
			t.Fatal("статус в фильтре: у FBS и FBO наборы значений разные")
		}
		windows++
		_, _ = response.Write([]byte(`{"result":{"postings":[{"created_at":"2026-08-05T10:00:00Z","status":"delivered","products":[{"offer_id":"OZ-1","quantity":1,"price":"100"}]}]}}`))
	}))
	defer server.Close()

	executor := NewMarketplaceExecutor("", "client", "secret")
	executor.ozonBase, executor.client = server.URL, server.Client()
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	records, err := executor.FetchSales(context.Background(), "ozon", from, to)
	if err != nil {
		t.Fatalf("продажи Ozon: %v", err)
	}
	// Двести двадцать два дня — это восемь окон по тридцать дней.
	if windows != 8 {
		t.Fatalf("окон = %d, ожидалось 8", windows)
	}
	if len(records) == 0 {
		t.Fatal("продаж не пришло")
	}
}
