package httpapi

import (
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/avpavlo8/ficusin-store/backend/internal/settings"
)

// analyticsSettings читает номер счётчика из настроек магазина. Отдельный
// узкий интерфейс, чтобы статика не получала право менять настройки.
type analyticsSettings interface {
	Value(key string) string
}

// setStaticCaching tells the browser how long it may keep a file.
//
// Files under /assets/ carry a content hash in their name, so a given URL
// never changes and may be kept forever. The service worker is the opposite
// case: it decides what everything else caches, so it must be re-fetched
// every time, or a bad worker would be impossible to replace.
func setStaticCaching(response http.ResponseWriter, path string) {
	switch {
	case path == "/sw.js":
		response.Header().Set("Cache-Control", "no-cache")
		// Without this the worker may only control /assets/, not the site.
		response.Header().Set("Service-Worker-Allowed", "/")
	case strings.HasPrefix(path, "/assets/"):
		response.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	case path == "/manifest.webmanifest":
		response.Header().Set("Content-Type", "application/manifest+json; charset=utf-8")
		response.Header().Set("Cache-Control", "public, max-age=3600")
	}
}

func spaFallback(
	logger *slog.Logger,
	api http.Handler,
	staticDir string,
	sitemap http.Handler,
	feeds http.Handler,
	products productMetaCatalog,
	landings sitemapCatalog,
	collections sitemapCollections,
	analytics analyticsSettings,
	configuredBase string,
) http.Handler {
	files := http.FileServer(http.Dir(staticDir))

	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if strings.HasPrefix(request.URL.Path, "/api/") {
			api.ServeHTTP(response, request)
			return
		}

		// Карту сайта собирает обработчик, а не файл на диске: каталог
		// меняется каждый день.
		if request.URL.Path == "/sitemap.xml" && sitemap != nil {
			sitemap.ServeHTTP(response, request)
			return
		}

		// Загрузчик счётчика отдаётся со своего адреса. Политика безопасности
		// запрещает встроенные скрипты, а ослаблять её ради аналитики нельзя:
		// unsafe-inline открыл бы дорогу любому внедрённому скрипту. Номер
		// читается на каждый запрос, поэтому смена в панели действует сразу.
		if request.URL.Path == "/analytics.js" {
			script := ""
			if analytics != nil {
				script = analyticsScript(analytics.Value(settings.MetrikaID))
			}
			response.Header().Set("Content-Type", "application/javascript; charset=utf-8")
			response.Header().Set("Cache-Control", "no-cache")
			_, _ = response.Write([]byte(script))
			return
		}
		if (request.URL.Path == "/feeds/google-products.xml" || request.URL.Path == "/feeds/yandex.yml") && feeds != nil {
			feeds.ServeHTTP(response, request)
			return
		}

		requestedPath := filepath.Join(staticDir, filepath.Clean(request.URL.Path))
		if info, err := os.Stat(requestedPath); err == nil && !info.IsDir() {
			setStaticCaching(response, request.URL.Path)
			files.ServeHTTP(response, request)
			return
		}

		indexPath := filepath.Join(staticDir, "index.html")
		body, err := os.ReadFile(indexPath)
		if err != nil {
			if os.IsNotExist(err) {
				http.NotFound(response, request)
				return
			}
			http.Error(response, fs.ErrInvalid.Error(), http.StatusInternalServerError)
			return
		}

		// Приложение рисует страницу в браузере, поэтому выдуманному адресу
		// отдаётся та же оболочка — но с честным кодом. Иначе поисковик
		// принимает опечатку в ссылке за настоящую страницу магазина.
		status := http.StatusOK
		base := canonicalBase(configuredBase)
		if !knownAppRoute(request.URL.Path) {
			status = http.StatusNotFound
		}
		if landingSlug(request.URL.Path, "/catalog/") != "" || landingSlug(request.URL.Path, "/collections/") != "" {
			var found bool
			body, found = withLandingMeta(request.Context(), logger, landings, collections, base, request.URL.Path, body)
			if !found {
				status = http.StatusNotFound
				body = withRouteMeta(base, "/__not_found__", body)
			}
		} else {
			body = withRouteMeta(base, request.URL.Path, body)
		}
		if slug := productSlug(request.URL.Path); slug != "" {
			var canonicalSlug string
			var productErr error
			body, canonicalSlug, productErr = withProductMeta(request.Context(), logger, products, base, slug, body)
			if productErr != nil {
				status = http.StatusServiceUnavailable
				body = withRouteMeta(base, "/__not_found__", body)
			} else if canonicalSlug == "" {
				status = http.StatusNotFound
				body = withRouteMeta(base, "/__not_found__", body)
			} else if canonicalSlug != slug {
				target := base + "/product/" + url.PathEscape(canonicalSlug)
				if request.URL.RawQuery != "" {
					target += "?" + request.URL.RawQuery
				}
				http.Redirect(response, request, target, http.StatusMovedPermanently)
				return
			}
		}
		// Счётчик ставится здесь, а не в сборке фронта: номер меняется в
		// панели без выкладки, а образ не носит в себе чужой аккаунт.
		if analytics != nil && !strings.HasPrefix(request.URL.Path, "/admin") {
			body = withAnalytics(body, analytics.Value(settings.MetrikaID))
			body = withSearchVerification(body, analytics.Value(settings.YandexVerification), analytics.Value(settings.GoogleVerification))
		}

		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		response.Header().Set("Cache-Control", "no-cache")
		response.WriteHeader(status)
		_, _ = response.Write(body)
	})
}
