package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestStagingGuardDisallowsIndexing(t *testing.T) {
	calls := 0
	h := EnvironmentGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(http.StatusNoContent) }), "staging", "test-access-token")
	for _, path := range []string{"/robots.txt", "/api/v1/ready", "/"} {
		r := httptest.NewRecorder()
		request := httptest.NewRequest("GET", path, nil)
		request.SetBasicAuth("staging", "test-access-token")
		h.ServeHTTP(r, request)
		if r.Header().Get("X-Ficusin-Environment") != "staging" || !strings.Contains(r.Header().Get("X-Robots-Tag"), "noindex") {
			t.Fatal("staging marker/indexing guard missing")
		}
		if path == "/robots.txt" && !strings.Contains(r.Body.String(), "Disallow: /") {
			t.Fatal("staging robots allow indexing")
		}
	}
	if calls != 2 {
		t.Fatal("robots must not reach the production static handler")
	}
	unauthorized := httptest.NewRecorder()
	h.ServeHTTP(unauthorized, httptest.NewRequest("GET", "/admin", nil))
	if unauthorized.Code != http.StatusSeeOther || calls != 2 {
		t.Fatal("anonymous staging access was allowed")
	}
}

func TestStagingBrowserSessionSurvivesApplicationUnauthorized(t *testing.T) {
	const secret = "staging-test-secret-with-at-least-32-characters"
	calls := 0
	handler := EnvironmentGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == "/api/v1/auth/me" {
			writeJSON(w, http.StatusUnauthorized, errorResponse{Error: "not logged in"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}), "staging", secret)
	login := httptest.NewRequest(http.MethodPost, stagingLoginPath, strings.NewReader(url.Values{"username": {"staging"}, "password": {secret}, "next": {"/admin?section=products"}}.Encode()))
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, login)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/admin?section=products" {
		t.Fatalf("login status=%d", response.Code)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("session cookie missing")
	}
	cookie := cookies[0]
	if !cookie.Secure || !cookie.HttpOnly || cookie.Path != "/" || cookie.MaxAge != 7*86400 || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatal("unsafe cookie")
	}
	for _, path := range []string{"/", "/api/v1/auth/me", "/admin", "/api/v1/auth/me", "/"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.AddCookie(cookie)
		reply := httptest.NewRecorder()
		handler.ServeHTTP(reply, request)
		expected := http.StatusNoContent
		if path == "/api/v1/auth/me" {
			expected = http.StatusUnauthorized
		}
		if reply.Code != expected || reply.Header().Get("WWW-Authenticate") != "" {
			t.Fatalf("%s: status=%d challenge=%q", path, reply.Code, reply.Header().Get("WWW-Authenticate"))
		}
	}
	if calls != 5 {
		t.Fatal("cookie did not authorize repeated visits")
	}
	cookie.Value += "tampered"
	request := httptest.NewRequest(http.MethodGet, "/admin", nil)
	request.AddCookie(cookie)
	reply := httptest.NewRecorder()
	handler.ServeHTTP(reply, request)
	if reply.Code != http.StatusSeeOther || calls != 5 {
		t.Fatal("tampered session accepted")
	}
}

func TestStagingLoginNeverChallengesOrRedirectsOffSite(t *testing.T) {
	handler := EnvironmentGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("anonymous reached handler") }), "staging", "secret")
	request := httptest.NewRequest(http.MethodGet, "/api/v1/catalog", nil)
	reply := httptest.NewRecorder()
	handler.ServeHTTP(reply, request)
	if reply.Code != http.StatusForbidden || reply.Header().Get("WWW-Authenticate") != "" {
		t.Fatal("anonymous API produced a browser challenge")
	}
	for _, value := range []string{"https://example.com", "//example.com", "/\\example.com", "/__staging/login"} {
		if stagingNext(value) != "/" {
			t.Fatalf("unsafe redirect %q", value)
		}
	}
	expired := strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10)
	request = httptest.NewRequest(http.MethodGet, "/admin", nil)
	request.AddCookie(&http.Cookie{Name: stagingCookieName, Value: expired + "." + stagingSignature("secret", expired)})
	reply = httptest.NewRecorder()
	handler.ServeHTTP(reply, request)
	if reply.Code != http.StatusSeeOther {
		t.Fatal("expired session accepted")
	}
}
