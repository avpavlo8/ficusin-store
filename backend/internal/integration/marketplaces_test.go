package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/procurement"
)

func TestOzonPriceIsConfirmedPerProduct(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Client-Id") != "client" || request.Header.Get("Api-Key") != "secret" {
			t.Fatal("Ozon credentials are missing")
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"result":[{"offer_id":"OZ-1","updated":true,"errors":[]}]}`))
	}))
	defer server.Close()
	executor := NewMarketplaceExecutor("", "client", "secret")
	executor.ozonBase, executor.client = server.URL, server.Client()
	result, err := executor.Execute(context.Background(), procurement.ActionItem{Channel: "ozon", ExternalArticle: "OZ-1", NewValue: 1490})
	if err != nil || !result.Completed {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
}

func TestWBSalesIncludeSalesAndSubtractReturns(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`[
			{"nm_id":123,"doc_type_name":"Продажа","supplier_oper_name":"Продажа","quantity":2,"retail_amount":3000,"sale_dt":"2026-08-05T12:00:00Z"},
			{"nm_id":123,"doc_type_name":"Возврат","supplier_oper_name":"Возврат","quantity":1,"retail_amount":1500,"sale_dt":"2026-08-06T12:00:00Z"}
		]`))
	}))
	defer server.Close()
	executor := NewMarketplaceExecutor("token", "", "")
	executor.wbStatsBase, executor.client = server.URL, server.Client()
	records, err := executor.FetchSales(context.Background(), "wb", time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC))
	if err != nil || len(records) != 2 || records[0].Units != 2 || records[1].Units != -1 {
		t.Fatalf("records = %+v, err = %v", records, err)
	}
}

func TestOzonSalesCombineFBSAndFBO(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/v3/posting/fbs/list" {
			_, _ = response.Write([]byte(`{"result":{"postings":[{"created_at":"2026-08-05T12:00:00Z","status":"delivered","products":[{"offer_id":"OZ-1","quantity":2,"price":"1200"}]}]}}`))
			return
		}
		_, _ = response.Write([]byte(`{"result":[{"created_at":"2026-08-06T12:00:00Z","status":"delivered","products":[{"offer_id":"OZ-1","quantity":1,"price":"1200"}]}]}`))
	}))
	defer server.Close()
	executor := NewMarketplaceExecutor("", "client", "secret")
	executor.ozonBase, executor.client = server.URL, server.Client()
	records, err := executor.FetchSales(context.Background(), "ozon", time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC))
	if err != nil || len(records) != 2 || records[0].ExternalID != "OZ-1" || records[1].Units != 1 {
		t.Fatalf("records = %+v, err = %v", records, err)
	}
}

func TestWBSubmissionIsCheckedBeforeCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodPost {
			_, _ = response.Write([]byte(`{"data":{"id":42,"alreadyExists":false},"error":false,"errorText":""}`))
			return
		}
		_, _ = response.Write([]byte(`{"data":{"uploadID":42,"status":3},"error":false,"errorText":""}`))
	}))
	defer server.Close()
	executor := NewMarketplaceExecutor("token", "", "")
	executor.wbBase, executor.client = server.URL, server.Client()
	first, err := executor.Execute(context.Background(), procurement.ActionItem{Channel: "wb", ExternalArticle: "123", NewValue: 1990})
	if err != nil || first.Completed || first.ExternalOperationID != "42" {
		t.Fatalf("submission = %#v, err = %v", first, err)
	}
	second, err := executor.Execute(context.Background(), procurement.ActionItem{Channel: "wb", ExternalArticle: "123", NewValue: 1990, ExternalOperationID: "42"})
	if err != nil || !second.Completed {
		t.Fatalf("confirmation = %#v, err = %v", second, err)
	}
}

func TestWBRejectsSellerArticleInsteadOfNmID(t *testing.T) {
	executor := NewMarketplaceExecutor("token", "", "")
	_, err := executor.Execute(context.Background(), procurement.ActionItem{Channel: "wb", ExternalArticle: "X123", NewValue: 1000})
	if err == nil {
		t.Fatal("non-numeric WB article must not be sent as nmID")
	}
}

func TestMarketplaceProbesAreReadOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/ping":
			if request.Method != http.MethodGet {
				t.Fatalf("WB probe method = %s", request.Method)
			}
			_, _ = response.Write([]byte(`{"Status":"OK"}`))
		case "/v3/product/list":
			if request.Method != http.MethodPost {
				t.Fatalf("Ozon probe method = %s", request.Method)
			}
			_, _ = response.Write([]byte(`{"result":{"items":[]}}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	executor := NewMarketplaceExecutor("token", "client", "secret")
	executor.wbBase, executor.ozonBase, executor.client = server.URL, server.URL, server.Client()
	for _, channel := range []string{"wb", "ozon"} {
		if err := executor.Probe(context.Background(), channel); err != nil {
			t.Fatalf("probe %s: %v", channel, err)
		}
	}
}
