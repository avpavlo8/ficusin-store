package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// Browser URLs stay same-origin: CSP, cookies and service-worker scope do not
// change when the physical asset bytes move to S3. Only safe request headers
// reach the public storage origin; credentials are never forwarded.
func staticAssetProxy(base string, fallback http.Handler) http.Handler {
	target, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil || target.Host == "" {
		return fallback
	}
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			fallback.ServeHTTP(w, r)
			return
		}
		remote := *target
		remote.Path = strings.TrimRight(target.Path, "/") + "/" + strings.TrimLeft(path.Clean("/"+r.URL.Path), "/")
		remote.RawQuery = ""
		remote.Fragment = ""
		remote.RawPath = ""
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		request, err := http.NewRequestWithContext(ctx, r.Method, remote.String(), nil)
		if err != nil {
			fallback.ServeHTTP(w, r)
			return
		}
		for _, header := range []string{"Range", "If-Range", "If-None-Match", "If-Modified-Since", "Accept"} {
			if v := r.Header.Get(header); v != "" {
				request.Header.Set(header, v)
			}
		}
		response, err := client.Do(request)
		if err != nil {
			fallback.ServeHTTP(w, r)
			return
		}
		defer response.Body.Close()
		if response.StatusCode != 200 && response.StatusCode != 206 && response.StatusCode != 304 {
			fallback.ServeHTTP(w, r)
			return
		}
		for _, header := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified", "Cache-Control"} {
			if v := response.Header.Get(header); v != "" {
				w.Header().Set(header, v)
			}
		}
		w.Header().Set("X-Ficusin-Asset-Source", "s3")
		w.WriteHeader(response.StatusCode)
		if r.Method != http.MethodHead {
			_, _ = io.Copy(w, response.Body)
		}
	})
}
func staticAssetCanUseS3(path string) bool {
	switch path {
	case "/sw.js", "/index.html", "/offline.html", "/manifest.webmanifest", "/admin.webmanifest", "/robots.txt":
		return false
	}
	return !strings.HasSuffix(path, ".html") && !strings.HasSuffix(path, ".webmanifest")
}

func staticAssetPath(value string) bool {
	if strings.HasPrefix(value, "/assets/") {
		return true
	}
	switch strings.ToLower(filepath.Ext(value)) {
	case ".js", ".css", ".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp", ".ico", ".woff", ".woff2", ".ttf", ".pdf", ".mp4", ".webm":
		return true
	}
	return false
}
