package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The current FBS/FBO contracts share top-level postings, structured prices
// and cursor pagination. A short page still continues when has_next is true.
func TestOzonCurrentPostingContractsReadEveryCursorPage(t *testing.T) {
	calls := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v4/posting/fbs/list" && r.URL.Path != "/v3/posting/fbo/list" {
			t.Errorf("retired or unexpected endpoint: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		var request map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		if string(request["limit"]) != "100" || string(request["sort_dir"]) != `"ASC"` || request["offset"] != nil || request["dir"] != nil {
			t.Errorf("wrong current posting request: %s", request)
		}
		if r.Header.Get("Client-Id") != "client" || r.Header.Get("Api-Key") != "secret" {
			t.Error("missing Ozon authentication headers")
		}
		var cursor string
		if err := json.Unmarshal(request["cursor"], &cursor); err != nil {
			t.Error(err)
		}
		calls[r.URL.Path]++
		w.Header().Set("Content-Type", "application/json")
		switch cursor {
		case "":
			fmt.Fprintf(w, `{"has_next":true,"cursor":"second","postings":[{"posting_number":%q,"status":"delivered","in_process_at":"2026-11-01T10:00:00Z","products":[{"offer_id":"PLANT-1","quantity":2,"price":{"amount":"19.90","currency":"RUB"}}]}]}`, r.URL.Path+"-1")
		case "second":
			fmt.Fprint(w, `{"has_next":true,"cursor":"third","postings":[]}`)
		case "third":
			fmt.Fprintf(w, `{"has_next":false,"cursor":"","postings":[{"posting_number":%q,"status":"delivering","created_at":"2026-11-01T12:00:00Z","products":[{"offer_id":"PLANT-2","quantity":3,"price":{"amount":"12.50","currency":"RUB"}}]}]}`, r.URL.Path+"-2")
		default:
			t.Errorf("unexpected cursor: %q", cursor)
		}
	}))
	defer server.Close()
	executor := NewMarketplaceExecutor("", "client", "secret")
	executor.ozonBase, executor.client = server.URL, server.Client()
	day := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	records, err := executor.FetchSales(context.Background(), "ozon", day, day)
	if err != nil || len(records) != 4 {
		t.Fatalf("records=%+v err=%v", records, err)
	}
	for _, path := range []string{"/v4/posting/fbs/list", "/v3/posting/fbo/list"} {
		if calls[path] != 3 {
			t.Errorf("%s: read %d pages instead of 3", path, calls[path])
		}
	}
	if records[0].ExternalID != "PLANT-1" || records[0].Units != 2 || math.Abs(records[0].GrossRUB-39.8) > 0.00001 || records[1].GrossRUB != 37.5 {
		t.Fatalf("structured prices or seller article were lost: %+v", records)
	}
}

func TestOzonBrokenCursorOrResponseCannotFinishSuccessfully(t *testing.T) {
	for _, tc := range []struct{ name, first, next, fragment string }{
		{"missing cursor", `{"postings":[],"has_next":true}`, "", "cursor"},
		{"repeated cursor", `{"postings":[],"has_next":true,"cursor":"same"}`, `{"postings":[],"has_next":true,"cursor":"same"}`, "cursor"},
		{"missing postings", `{}`, "", "postings"},
		{"old envelope", `{"result":{"postings":[]}}`, "", "postings"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				if calls == 1 {
					fmt.Fprint(w, tc.first)
				} else {
					fmt.Fprint(w, tc.next)
				}
			}))
			defer server.Close()
			executor := NewMarketplaceExecutor("", "client", "secret")
			executor.ozonBase, executor.client = server.URL, server.Client()
			day := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
			items, err := executor.fetchOzonPostingWindow(context.Background(), ozonFBSPostingsPath, day, day, 100)
			if err == nil || !strings.Contains(err.Error(), tc.fragment) || items != nil || calls > 2 {
				t.Fatalf("partial or invalid result accepted: items=%+v calls=%d err=%v", items, calls, err)
			}
		})
	}
}

func TestOzonLaterPageFailureDiscardsPartialSales(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Cursor string `json:"cursor"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		if request.Cursor != "" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"code":3,"message":"page failed"}`)
			return
		}
		fmt.Fprint(w, `{"has_next":true,"cursor":"next","postings":[{"posting_number":"A","status":"delivered","created_at":"2026-11-01T12:00:00Z","products":[{"offer_id":"PLANT-1","quantity":1,"price":{"amount":"12.50","currency":"RUB"}}]}]}`)
	}))
	defer server.Close()
	executor := NewMarketplaceExecutor("", "client", "secret")
	executor.ozonBase, executor.client = server.URL, server.Client()
	day := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	records, err := executor.FetchSales(context.Background(), "ozon", day, day)
	if err == nil || records != nil {
		t.Fatalf("partial sales accepted: %+v, %v", records, err)
	}
}
