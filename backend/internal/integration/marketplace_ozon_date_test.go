package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Список FBS третьей версии не отдаёт created_at. Пока дату брали только
// оттуда, все шесть тысяч отправлений молча пропадали на разборе даты.
// До суток дату округляет уже хранилище продаж, выгрузка отдаёт время как есть.
func TestOzonPostingWithoutCreatedAtStillCounts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if !strings.Contains(request.URL.Path, "/fbs/") {
			_, _ = response.Write([]byte(`{"result":[]}`))
			return
		}
		_, _ = response.Write([]byte(`{"result":{"postings":[{"in_process_at":"2026-08-01T10:00:00Z","shipment_date":"2026-08-02T10:00:00Z","status":"delivered","products":[{"offer_id":"OZ-1","quantity":3,"price":"100"}]}]}}`))
	}))
	defer server.Close()

	executor := NewMarketplaceExecutor("", "client", "secret")
	executor.ozonBase, executor.client = server.URL, server.Client()
	day := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	records, err := executor.FetchSales(context.Background(), "ozon", day, day)
	if err != nil {
		t.Fatalf("продажи Ozon: %v", err)
	}
	if len(records) != 1 || records[0].Units != 3 {
		t.Fatalf("строк = %d, ожидалась одна с тремя штуками: %#v", len(records), records)
	}
	expected := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	if !records[0].Date.Equal(expected) {
		t.Fatalf("дата = %s, ожидалась %s", records[0].Date, expected)
	}
}
