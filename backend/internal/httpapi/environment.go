package httpapi

import (
	"crypto/subtle"
	"net/http"
)

// Staging has an independent origin and database. Never index previews or let
// a CDN/browser reuse their responses after a candidate is replaced.
func EnvironmentGuard(next http.Handler, environment, accessToken string) http.Handler {
	if environment != "staging" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Ficusin-Environment", "staging")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive")
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Path == "/robots.txt" {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
			return
		}
		if r.URL.Path != "/api/v1/health" && r.URL.Path != "/api/v1/ready" {
			username, password, ok := r.BasicAuth()
			if !ok || username != "staging" || accessToken == "" || subtle.ConstantTimeCompare([]byte(password), []byte(accessToken)) != 1 {
				w.Header().Set("WWW-Authenticate", `Basic realm="Ficusin staging", charset="UTF-8"`)
				http.Error(w, "Staging access required", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
