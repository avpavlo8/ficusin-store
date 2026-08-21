package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func decodeJSON(t *testing.T, response *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	var payload map[string]string
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode json: %v; body=%q", err, response.Body.String())
	}
	return payload
}

func TestBootstrapHTTPHandler(t *testing.T) {
	t.Run("health is live while application is starting", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		response := httptest.NewRecorder()

		bootstrapHTTPHandler(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
		}
		if status := decodeJSON(t, response)["status"]; status != "starting" {
			t.Fatalf("status payload = %q, want starting", status)
		}
	})

	t.Run("business endpoints stay unavailable until bootstrap finishes", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/catalog", nil)
		response := httptest.NewRecorder()

		bootstrapHTTPHandler(response, request)

		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
		}
		if message := decodeJSON(t, response)["error"]; message == "" {
			t.Fatal("bootstrap 503 must include an error message")
		}
	})
}

func TestSwitchHandlerSwapsToReadyRouter(t *testing.T) {
	handler := newSwitchHandler(http.HandlerFunc(bootstrapHTTPHandler))
	handler.Swap(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(writer).Encode(map[string]string{"status": "ok"})
	}))

	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if status := decodeJSON(t, response)["status"]; status != "ok" {
		t.Fatalf("status payload = %q, want ok", status)
	}
}
