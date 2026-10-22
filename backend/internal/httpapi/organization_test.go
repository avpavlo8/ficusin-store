package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOrganizationSuggestionsProxy(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.Header.Get("Authorization") != "Token private-test-key" {
			t.Errorf("invalid authenticated provider request")
		}
		var body struct {
			Query  string   `json:"query"`
			Count  int      `json:"count"`
			Status []string `json:"status"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Query != "Пример" || body.Count != 7 || len(body.Status) != 1 || body.Status[0] != "ACTIVE" {
			t.Errorf("unexpected provider query: %+v", body)
		}
		io.WriteString(w, `{"suggestions":[{"value":"Пример","data":{"inn":"7707083893","kpp":"773601001","name":{"short_with_opf":"ООО Пример"},"address":{"unrestricted_value":"Нормализованный адрес","data":{"source":"Адрес из ЕГРЮЛ"}}}},{"value":"Филиал","data":{"inn":"7707083893","kpp":"773601002","address":{"value":"Адрес филиала"}}},{"value":"Дубликат","data":{"inn":"7707083893","kpp":"773601001"}},{"value":"Ошибка","data":{"inn":"abc"}}]}`)
	}))
	defer provider.Close()
	h := &organizationSuggester{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), key: "private-test-key", endpoint: provider.URL, client: provider.Client()}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/api/v1/organizations/suggest?q=Пример", nil))
	var response struct {
		Available   bool                     `json:"available"`
		Suggestions []organizationSuggestion `json:"suggestions"`
	}
	if json.Unmarshal(rr.Body.Bytes(), &response) != nil || rr.Code != 200 || !response.Available || len(response.Suggestions) != 2 {
		t.Fatalf("invalid response: %d %s", rr.Code, rr.Body.String())
	}
	if response.Suggestions[0].Name != "ООО Пример" || response.Suggestions[0].LegalAddress != "Адрес из ЕГРЮЛ" || response.Suggestions[1].KPP != "773601002" {
		t.Fatalf("incorrect or collapsed branch details: %+v", response.Suggestions)
	}
	if strings.Contains(rr.Body.String(), "private-test-key") || rr.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("unsafe public response")
	}
}

func TestOrganizationSuggestionsFallbacks(t *testing.T) {
	for _, test := range []struct {
		name, key, query, body     string
		providerStatus, wantStatus int
		wantCalls                  bool
	}{
		{"not configured", "", "пример", "", 200, 200, false},
		{"short unicode query", "key", "ОО", "", 200, 200, false},
		{"oversized query", "key", strings.Repeat("а", 151), "", 200, 400, false},
		{"provider quota exhausted", "key", "пример", "private-test-key", 403, 503, true},
		{"invalid JSON", "key", "пример", "not-json", 200, 503, true},
		{"no results", "key", "пример", `{"suggestions":[]}`, 200, 200, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(test.providerStatus)
				io.WriteString(w, test.body)
			}))
			defer server.Close()
			h := &organizationSuggester{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), key: test.key, endpoint: server.URL, client: server.Client()}
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, httptest.NewRequest("GET", "/api/v1/organizations/suggest?q="+test.query, nil))
			if rr.Code != test.wantStatus || (calls > 0) != test.wantCalls {
				t.Fatalf("status=%d calls=%d body=%s", rr.Code, calls, rr.Body.String())
			}
			if strings.Contains(rr.Body.String(), "private-test-key") {
				t.Fatal("provider response leaked")
			}
		})
	}
}

func TestOrganizationSuggestionsPublicRoute(t *testing.T) {
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{})
	for i := 0; i < 31; i++ {
		request := httptest.NewRequest("GET", "/api/v1/organizations/suggest?q=пример", nil)
		request.RemoteAddr = "192.0.2.1:12345"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, request)
		if i < 30 && rr.Code != 200 {
			t.Fatalf("unconfigured route: %d", rr.Code)
		}
		if i == 30 && rr.Code != http.StatusTooManyRequests {
			t.Fatalf("limiter did not protect lookup: %d", rr.Code)
		}
	}
}
