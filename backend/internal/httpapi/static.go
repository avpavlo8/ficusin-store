package httpapi

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

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
		response.Header().Set("Cache-Control", "public, max-age=3600")
	}
}

func spaFallback(api http.Handler, staticDir string, sitemap http.Handler) http.Handler {
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
		if !knownAppRoute(request.URL.Path) {
			status = http.StatusNotFound
		}
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		response.Header().Set("Cache-Control", "no-cache")
		response.WriteHeader(status)
		_, _ = response.Write(body)
	})
}
