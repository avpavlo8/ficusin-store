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
