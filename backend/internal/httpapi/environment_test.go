package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStagingGuardDisallowsIndexing(t *testing.T) {
	calls := 0
	h := EnvironmentGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(http.StatusNoContent) }), "staging")
	for _, path := range []string{"/robots.txt", "/api/v1/ready", "/"} {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", path, nil))
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
}
