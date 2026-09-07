package httpapi

import (
	"net/http"
	"net/url"
	"strings"
)

// contentSecurityPolicy describes where the page is allowed to load things
// from. Application code is served from our own origin. The optional Yandex
// Metrika adapter is the only browser-side third party; address suggestions
// and delivery quotes still go through our API rather than external hosts.
//
//   - img-src also allows https: because product photos come from the
//     catalogue's own storage, and data: because the avatar editor previews
//     the picked file before uploading it.
//   - style-src allows inline styles: React writes a few through the style
//     attribute, which the browser treats as inline.
//   - frame-ancestors none stops the site being framed, which is what makes
//     clickjacking possible.
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self' https://mc.yandex.ru; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: https:; " +
	"font-src 'self' data:; " +
	"connect-src 'self' https://mc.yandex.ru https://mc.yandex.com wss://mc.yandex.ru wss://mc.yandex.com; " +
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

// rejectCrossOriginMutations adds an explicit browser boundary for every
// cookie-authenticated mutation. Requests without Origin remain valid for
// server-to-server webhooks and command-line clients; browsers always attach
// Origin to a cross-site fetch, even when they cannot read the response.
func rejectCrossOriginMutations(siteURL string, next http.Handler) http.Handler {
	expected, _ := url.Parse(strings.TrimSpace(siteURL))
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet || request.Method == http.MethodHead || request.Method == http.MethodOptions {
			next.ServeHTTP(response, request)
			return
		}
		rawOrigin := strings.TrimSpace(request.Header.Get("Origin"))
		if rawOrigin == "" {
			next.ServeHTTP(response, request)
			return
		}
		origin, err := url.Parse(rawOrigin)
		host := request.Host
		scheme := "https"
		if expected != nil && expected.Host != "" {
			host = expected.Host
			scheme = expected.Scheme
		} else if request.TLS == nil {
			scheme = "http"
		}
		if err != nil || !strings.EqualFold(origin.Host, host) || !strings.EqualFold(origin.Scheme, scheme) {
			writeJSON(response, http.StatusForbidden, errorResponse{Error: "Запрос с другого сайта отклонён"})
			return
		}
		next.ServeHTTP(response, request)
	})
}
