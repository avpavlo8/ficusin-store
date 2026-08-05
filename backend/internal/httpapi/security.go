package httpapi

import (
	"net/http"
	"strings"
)

// contentSecurityPolicy describes where the page is allowed to load things
// from. Everything the store needs is served from our own origin: scripts
// and styles are built by Vite, address suggestions and delivery quotes go
// through our own API rather than straight to Yandex or CDEK.
//
//   - img-src also allows https: because product photos come from the
//     catalogue's own storage, and data: because the avatar editor previews
//     the picked file before uploading it.
//   - style-src allows inline styles: React writes a few through the style
//     attribute, which the browser treats as inline.
//   - frame-ancestors none stops the site being framed, which is what makes
//     clickjacking possible.
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: https:; " +
	"font-src 'self' data:; " +
	"connect-src 'self'; " +
	"manifest-src 'self'; " +
	"worker-src 'self'; " +
	"form-action 'self'; " +
	"base-uri 'self'; " +
	"frame-ancestors 'none'; " +
	"object-src 'none'"

// securityHeaders sets the headers a browser needs in order to defend the
// visitor. Without nosniff in particular, a browser may guess the type of
// an uploaded avatar from its bytes and run it as a document.
func securityHeaders(secure bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		header := response.Header()
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		header.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		header.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		if secure {
			header.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		// The policy only makes sense for pages; API responses are JSON and
		// carry no markup to protect.
		if !strings.HasPrefix(request.URL.Path, "/api/") {
			header.Set("Content-Security-Policy", contentSecurityPolicy)
		}
		next.ServeHTTP(response, request)
	})
}
