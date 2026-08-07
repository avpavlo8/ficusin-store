package httpapi

import (
	"context"
	"encoding/xml"
	"log/slog"
	"net/http"
	"strings"

	"github.com/avpavlo8/ficusin-store/backend/internal/catalog"
)

// Адреса, которые приложение рисует в браузере. Сервер знает о них ровно
// затем, чтобы отвечать «404» на выдуманный адрес и не отвечать им на
// настоящий: раньше любой набор букв возвращал «200 ОК», и поисковики
// заводили в индекс пустые страницы, которых у магазина нет.
var appRoutes = map[string]bool{
	"/":                     true,
	"/login":                true,
	"/register":             true,
	"/account":              true,
	"/account/profile":      true,
	"/account/favorites":    true,
	"/admin":                true,
	"/favorites":            true,
	"/offer":                true,
	"/privacy":              true,
	"/requisites":           true,
	"/delivery-and-returns": true,
}

func knownAppRoute(path string) bool {
	if appRoutes[path] {
		return true
	}
	// Карточка товара и отдельный заказ — адреса с переменной частью.
	return strings.HasPrefix(path, "/product/") || strings.HasPrefix(path, "/account/orders/")
}

type sitemapCatalog interface {
	ListAvailable(context.Context) ([]catalog.Product, error)
}

type sitemapURL struct {
	Location   string `xml:"loc"`
	Changefreq string `xml:"changefreq,omitempty"`
	Priority   string `xml:"priority,omitempty"`
}

type sitemapSet struct {
	XMLName xml.Name     `xml:"urlset"`
	Xmlns   string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

// Карта сайта собирается на лету. Файл, который надо обновлять руками,
// устареет к пятнице: каталог приезжает из Saby каждый день.
//
// Пустой каталог — не повод отдавать ошибку: статические страницы магазина
// существуют независимо от того, ответила ли база.
func sitemapHandler(logger *slog.Logger, repository sitemapCatalog) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		base := siteBase(request)
		set := sitemapSet{Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9"}
		for _, path := range []string{"/", "/delivery-and-returns", "/offer", "/privacy", "/requisites"} {
			set.URLs = append(set.URLs, sitemapURL{
				Location: base + path, Changefreq: "weekly", Priority: "0.8",
			})
		}
		if repository != nil {
			products, err := repository.ListAvailable(request.Context())
			if err != nil {
				logger.Error("sitemap catalog failed", "error", err)
			}
			for _, product := range products {
				set.URLs = append(set.URLs, sitemapURL{
					Location: base + "/product/" + product.ID, Changefreq: "daily", Priority: "0.6",
				})
			}
		}
		response.Header().Set("Content-Type", "application/xml; charset=utf-8")
		response.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = response.Write([]byte(xml.Header))
		_ = xml.NewEncoder(response).Encode(set)
	})
}

// За обратным прокси Timeweb схема приходит заголовком, иначе ссылки в карте
// сайта уедут на http и поисковик посчитает их отдельными страницами.
func siteBase(request *http.Request) string {
	scheme := "https"
	if forwarded := request.Header.Get("X-Forwarded-Proto"); forwarded != "" {
		scheme = forwarded
	} else if request.TLS == nil {
		scheme = "http"
	}
	return scheme + "://" + request.Host
}
