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


func TestOzonCountsShipmentAfterItLeavesSeller(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if !strings.Contains(request.URL.Path, "/fbs/") {
			_, _ = response.Write([]byte(`{"result":[]}`))
			return
		}
		_, _ = response.Write([]byte(`{"result":{"postings":[
			{"posting_number":"A","in_process_at":"2026-09-25T10:00:00Z","status":"awaiting_deliver","products":[{"offer_id":"OZ-1","quantity":1,"price":"100"}]},
			{"posting_number":"B","in_process_at":"2026-09-25T11:00:00Z","status":"driver_pickup","products":[{"offer_id":"OZ-1","quantity":1,"price":"100"}]},
			{"posting_number":"C","in_process_at":"2026-09-25T12:00:00Z","status":"delivering","products":[{"offer_id":"OZ-1","quantity":1,"price":"100"}]},
			{"posting_number":"D","in_process_at":"2026-09-25T13:00:00Z","status":"last_mile","products":[{"offer_id":"OZ-1","quantity":1,"price":"100"}]},
			{"posting_number":"E","in_process_at":"2026-09-25T14:00:00Z","status":"delivered","products":[{"offer_id":"OZ-1","quantity":1,"price":"100"}]},
			{"posting_number":"F","in_process_at":"2026-09-25T15:00:00Z","status":"cancelled","products":[{"offer_id":"OZ-1","quantity":1,"price":"100"}]}
		]}}`))
	}))
	defer server.Close()

	executor := NewMarketplaceExecutor("", "client", "secret")
	executor.ozonBase, executor.client = server.URL, server.Client()
	day := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	records, err := executor.FetchSales(context.Background(), "ozon", day, day)
	if err != nil {
		t.Fatalf("продажи Ozon: %v", err)
	}
	if len(records) != 4 {
		t.Fatalf("строк = %d, ожидались driver_pickup/delivering/last_mile/delivered: %#v", len(records), records)
	}
}
