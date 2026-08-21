package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBootstrapHTTPHandler(t *testing.T) {
	t.Run("health is live while application is starting", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		response := httptest.NewRecorder()

		bootstrapHTTPHandler(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
		}
		if strings.TrimSpace(response.Body.String()) != `{"status":"starting"}` {
			t.Fatalf("body = %q", response.Body.String())
		}
	})

	t.Run("business endpoints stay unavailable until bootstrap finishes", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/catalog", nil)
		response := httptest.NewRecorder()

		bootstrapHTTPHandler(response, request)

		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
		}
	})
}

func TestSwitchHandlerSwapsToReadyRouter(t *testing.T) {
	handler := newSwitchHandler(http.HandlerFunc(bootstrapHTTPHandler))
	handler.Swap(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(`{"status":"ok"}`))
	}))

	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if strings.TrimSpace(response.Body.String()) != `{"status":"ok"}` {
		t.Fatalf("body = %q", response.Body.String())
	}
}
