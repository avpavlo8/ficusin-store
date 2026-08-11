package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
