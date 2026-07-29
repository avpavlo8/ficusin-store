package httpapi

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func spaFallback(api http.Handler, staticDir string) http.Handler {
	files := http.FileServer(http.Dir(staticDir))

	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if strings.HasPrefix(request.URL.Path, "/api/") {
			api.ServeHTTP(response, request)
			return
		}

		requestedPath := filepath.Join(staticDir, filepath.Clean(request.URL.Path))
		if info, err := os.Stat(requestedPath); err == nil && !info.IsDir() {
			files.ServeHTTP(response, request)
			return
		}

		indexPath := filepath.Join(staticDir, "index.html")
		if _, err := os.Stat(indexPath); err != nil {
			if os.IsNotExist(err) {
				http.NotFound(response, request)
				return
			}
			http.Error(response, fs.ErrInvalid.Error(), http.StatusInternalServerError)
			return
		}
		http.ServeFile(response, request, indexPath)
	})
}
