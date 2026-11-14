package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeadersAllowLocalMediaPreviewsWithoutExecutableBlobs(t *testing.T) {
	response := httptest.NewRecorder()
	securityHeaders(true, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/admin?section=products&create=1", nil))
	directives := map[string]string{}
	for _, directive := range strings.Split(response.Header().Get("Content-Security-Policy"), ";") {
		fields := strings.Fields(directive)
		if len(fields) > 0 {
			directives[fields[0]] = strings.Join(fields[1:], " ")
		}
	}
	for _, name := range []string{"img-src", "media-src"} {
		if !strings.Contains(" "+directives[name]+" ", " blob: ") {
			t.Errorf("%s blocks local draft previews: %q", name, directives[name])
		}
	}
	for _, name := range []string{"default-src", "script-src", "worker-src", "connect-src"} {
		if strings.Contains(directives[name], "blob:") {
			t.Errorf("%s must not allow executable blob URLs", name)
		}
	}
}

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

func TestSecurityHeadersScopeMapFramesToYandex(t *testing.T) {
	response := httptest.NewRecorder()
	securityHeaders(true, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/contacts", nil))
	policy := response.Header().Get("Content-Security-Policy")
	if !strings.Contains(policy, "frame-src 'self' https://yandex.ru;") || !strings.Contains(policy, "frame-ancestors 'none';") {
		t.Fatalf("map frame permission or framing protection missing: %q", policy)
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
