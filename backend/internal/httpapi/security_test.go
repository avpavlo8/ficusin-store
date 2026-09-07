package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeadersAllowYandexMetrikaWebSockets(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/product/5", nil)
	response := httptest.NewRecorder()

	securityHeaders(true, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(response, request)

	policy := response.Header().Get("Content-Security-Policy")
	for _, source := range []string{"wss://mc.yandex.ru", "wss://mc.yandex.com"} {
		if !strings.Contains(policy, source) {
			t.Fatalf("Content-Security-Policy does not allow %s: %q", source, policy)
		}
	}
}

func TestCrossOriginMutationIsRejected(t *testing.T) {
	request := httptest.NewRequest(http.MethodPut, "https://ficusin.ru/api/v1/cart", strings.NewReader(`{"items":{}}`))
	request.Header.Set("Origin", "https://attacker.example")
	response := httptest.NewRecorder()
	called := false
	rejectCrossOriginMutations("https://ficusin.ru", http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })).ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || called {
		t.Fatalf("cross-origin mutation status=%d called=%v", response.Code, called)
	}
}

func TestSameOriginAndServerMutationsAreAllowed(t *testing.T) {
	for _, origin := range []string{"https://ficusin.ru", ""} {
		request := httptest.NewRequest(http.MethodPost, "https://ficusin.ru/api/v1/orders", nil)
		if origin != "" {
			request.Header.Set("Origin", origin)
		}
		response := httptest.NewRecorder()
		called := false
		rejectCrossOriginMutations("https://ficusin.ru", http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })).ServeHTTP(response, request)
		if !called {
			t.Fatalf("origin %q was rejected: %d", origin, response.Code)
		}
	}
}
