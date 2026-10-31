package httpapi

import "net/http"

// Staging has an independent origin and database. Never index previews or let
// a CDN/browser reuse their responses after a candidate is replaced.
func EnvironmentGuard(next http.Handler, environment string) http.Handler {
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
		next.ServeHTTP(w, r)
	})
}
