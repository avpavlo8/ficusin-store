package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

func TestSabyCatalogOutcomeAcceptsObjectShape(t *testing.T) {
	var page sabyCatalogPage
	if err := json.Unmarshal([]byte(`{"outcome":{"hasMore":false}}`), &page); err != nil {
		t.Fatal(err)
	}
	if hasMore, known := page.hasMore(); !known || hasMore {
		t.Fatalf("hasMore=%v known=%v, want false/true", hasMore, known)
	}
}

func TestSabyCatalogOutcomeAcceptsBooleanShape(t *testing.T) {
	var page sabyCatalogPage
	if err := json.Unmarshal([]byte(`{"outcome":true}`), &page); err != nil {
		t.Fatal(err)
	}
	if hasMore, known := page.hasMore(); !known || !hasMore {
		t.Fatalf("hasMore=%v known=%v, want true/true", hasMore, known)
	}
}

func TestSabyCatalogPaginationUsesObjectOutcomeAsEOF(t *testing.T) {
	var catalogueCalls int
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/oauth/service/" {
			_, _ = response.Write([]byte(`{"token":"safe-token"}`))
			return
		}
		catalogueCalls++
		if request.URL.Query().Get("page") != "0" {
			t.Errorf("unexpected page after outcome.hasMore=false: %s", request.URL.Query().Get("page"))
		}
		_, _ = response.Write([]byte(`{"nomenclatures":[{"id":"101","name":"Anthurium Beauty Black"}],"outcome":{"hasMore":false}}`))
	}))
	defer server.Close()

	client := NewSabyClient("client", "secret", "service", 278, 6)
	client.authURL, client.apiBase, client.client = server.URL+"/oauth/service/", server.URL, server.Client()
	rows, err := client.fetchCatalogSection(context.Background(), url.Values{"pointId": {"278"}, "pageSize": {"1000"}}, "", map[string]bool{}, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || catalogueCalls != 1 {
		t.Fatalf("rows=%d calls=%d, want one page", len(rows), catalogueCalls)
	}
}

func TestSabyCatalogPaginationOutcomeKeepsDuplicateBridgePage(t *testing.T) {
	var catalogueCalls int
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/oauth/service/" {
			_, _ = response.Write([]byte(`{"token":"safe-token"}`))
			return
		}
		catalogueCalls++
		page, _ := strconv.Atoi(request.URL.Query().Get("page"))
		switch page {
		case 0:
			_, _ = response.Write([]byte(`{"nomenclatures":[{"id":"101","name":"Anthurium Beauty Black"}],"outcome":{"hasMore":true}}`))
		case 1:
			_, _ = response.Write([]byte(`{"nomenclatures":[{"id":"101","name":"Anthurium Beauty Black"}],"outcome":{"hasMore":true}}`))
		case 2:
			_, _ = response.Write([]byte(`{"nomenclatures":[{"id":"102","name":"Anthurium Black Love"},{"id":"103","name":"Anthurium Melodia Ibis"}],"outcome":{"hasMore":false}}`))
		default:
			t.Errorf("unexpected page %d", page)
			_, _ = response.Write([]byte(`{"nomenclatures":[],"outcome":{"hasMore":false}}`))
		}
	}))
	defer server.Close()

	client := NewSabyClient("client", "secret", "service", 278, 6)
	client.authURL, client.apiBase, client.client = server.URL+"/oauth/service/", server.URL, server.Client()
	rows, err := client.fetchCatalogSection(context.Background(), url.Values{"pointId": {"278"}, "pageSize": {"1000"}}, "", map[string]bool{}, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || catalogueCalls != 3 {
		t.Fatalf("rows=%d calls=%d, want three rows across three pages", len(rows), catalogueCalls)
	}
}

func TestSabyCatalogPaginationStopsAfterRepeatedNoProgressWithoutOutcome(t *testing.T) {
	var catalogueCalls int
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/oauth/service/" {
			_, _ = response.Write([]byte(`{"token":"safe-token"}`))
			return
		}
		catalogueCalls++
		_, _ = response.Write([]byte(`{"nomenclatures":[{"id":"101","name":"Anthurium Beauty Black"}]}`))
	}))
	defer server.Close()

	client := NewSabyClient("client", "secret", "service", 278, 6)
	client.authURL, client.apiBase, client.client = server.URL+"/oauth/service/", server.URL, server.Client()
	rows, err := client.fetchCatalogSection(context.Background(), url.Values{"pointId": {"278"}, "pageSize": {"1000"}}, "", map[string]bool{}, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || catalogueCalls != 6 {
		t.Fatalf("rows=%d calls=%d, want one row and bounded fallback pagination", len(rows), catalogueCalls)
	}
}
