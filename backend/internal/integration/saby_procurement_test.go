package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestSabyProbeCachesServiceSession(t *testing.T) {
	var authCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/oauth/service/" {
			authCalls.Add(1)
			_, _ = response.Write([]byte(`{"token":"safe-token"}`))
			return
		}
		if request.Header.Get("X-SBISAccessToken") != "safe-token" {
			t.Fatal("Saby access token is missing")
		}
		if request.URL.Query().Get("pointId") != "278" || request.URL.Query().Get("priceListId") != "6" || request.URL.Query().Get("pageSize") != "1" {
			t.Fatalf("unexpected safe probe query: %s", request.URL.RawQuery)
		}
		_, _ = response.Write([]byte(`{"nomenclatures":[]}`))
	}))
	defer server.Close()

	client := NewSabyClient("client", "secret", "service", 278, 6)
	client.authURL, client.apiBase, client.client = server.URL+"/oauth/service/", server.URL, server.Client()
	if err := client.Probe(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := client.Probe(context.Background()); err != nil {
		t.Fatal(err)
	}
	if authCalls.Load() != 1 {
		t.Fatalf("auth calls = %d, want 1", authCalls.Load())
	}
}
