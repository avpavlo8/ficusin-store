package httpapi

import (
	"bytes"
	"net/http"
	"strings"
)

func adminAppRequest(request *http.Request) bool {
	path := request.URL.Path
	if path == "/admin" || strings.HasPrefix(path, "/admin/") {
		return true
	}
	if path == "/login" || path == "/register" {
		target := request.URL.Query().Get("returnTo")
		return target == "/admin" || strings.HasPrefix(target, "/admin?") || strings.HasPrefix(target, "/admin/")
	}
	return false
}

// Set identity in the initial HTML, before Safari or Chromium reads the
// manifest. Both installed apps share the existing session and service worker.
func withAdminAppMeta(shell []byte) []byte {
	page := titlePattern.ReplaceAllLiteral(shell, []byte("<title>Фикусин — управление</title>"))
	page = descriptionPattern.ReplaceAllLiteral(page, []byte(`<meta name="description" content="Заказы, закупки и каталог Фикусина. Приложение для сотрудников.">`))
	for _, replacement := range [][2]string{
		{"/manifest.webmanifest?v=app-logo-v2", "/admin.webmanifest?v=admin-v1"},
		{"/assets/brand/graphite-v1/app-180-v2.png", "/assets/brand/admin-v1/app-180.png"},
		{`name="apple-mobile-web-app-title" content="Фикусин"`, `name="apple-mobile-web-app-title" content="Управление"`},
		{`name="theme-color" content="#353630"`, `name="theme-color" content="#244734"`},
		{"/favicon.svg?v=app-logo-v2", "/assets/brand/admin-v1/app.svg"},
	} {
		page = bytes.ReplaceAll(page, []byte(replacement[0]), []byte(replacement[1]))
	}
	return page
}
