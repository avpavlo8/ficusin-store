package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSabySalesUseRetailReceiptsAndStableDocuments(t *testing.T) {
	var listCalls int
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/oauth/service/":
			_, _ = response.Write([]byte(`{"token":"sales-token"}`))
		case "/retail/order/list":
			listCalls++
			if request.Header.Get("X-SBISAccessToken") != "sales-token" {
				t.Fatal("missing Saby token")
			}
			query := request.URL.Query()
			if query.Get("pointId") != "278" || query.Get("page") != "0" || query.Get("pageSize") != "100" {
				t.Fatalf("unexpected query: %s", request.URL.RawQuery)
			}
			_, _ = response.Write([]byte(`{"orders":[
				{"UUID":"sale-1","ClosedWTZ":"2026-09-14 12:00:00","SaleNomenclatures":[
					{"UUID":"line-1","NomenclatureUUID":"plant-uuid","Quantity":2,"TotalPrice":3200}
				]},
				{"UUID":"return-1","DateWTZ":"2026-09-14 16:00:00","Return":true,"Positions":[
					{"ID":"line-2","NomenclatureID":"plant-id","Quantity":1,"TotalPrice":1600}
				]}
			]}`))
		default:
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
	}))
	defer server.Close()

	client := NewSabyClient("client", "secret", "key", 278, 6)
	client.authURL, client.apiBase, client.client = server.URL+"/oauth/service/", server.URL, server.Client()
	day := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	records, err := client.FetchSales(context.Background(), day, day)
	if err != nil {
		t.Fatal(err)
	}
	if listCalls != 1 || len(records) != 2 {
		t.Fatalf("calls=%d records=%+v", listCalls, records)
	}
	if records[0].SourceDocumentID != "sale-1" || records[0].SourceLineID != "line-1" ||
		records[0].ExternalID != "plant-uuid" || records[0].Units != 2 ||
		records[0].GrossRUB != 3200 || records[0].EventType != "sale" {
		t.Fatalf("sale=%+v", records[0])
	}
	if records[1].SourceDocumentID != "return-1" || records[1].ExternalID != "plant-id" ||
		records[1].EventType != "return" || records[1].GrossRUB != 1600 {
		t.Fatalf("return=%+v", records[1])
	}
}

func TestSalesExecutorRoutesSabyThroughOneSource(t *testing.T) {
	executor := NewSalesExecutor(nil, NewSabyClient("client", "secret", "key", 278, 6))
	if !executor.Configured("saby") || executor.Configured("ozon") {
		t.Fatalf("unexpected routing state")
	}
}
