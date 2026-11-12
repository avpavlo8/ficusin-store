package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStaticS3ProxyKeepsSameOriginBoundaryAndFallsBack(t *testing.T) {
	calls := 0
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.URL.RawQuery != "" {
			t.Error("credentials or query leaked to S3")
		}
		if r.URL.Path != "/bucket/release/assets/app.js" {
			t.Error("wrong key", r.URL.Path)
		}
		if calls == 2 {
			w.WriteHeader(403)
			return
		}
		w.Header().Set("Content-Type", "text/javascript")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		io.WriteString(w, "from-s3")
	}))
	defer storage.Close()
	fallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "local-rollback") })
	h := staticAssetProxy(storage.URL+"/bucket/release", fallback)
	for i, want := range []string{"from-s3", "local-rollback"} {
		r := httptest.NewRequest("GET", "/assets/app.js?token=not-for-storage", nil)
		r.Header.Set("Cookie", "session=private")
		r.Header.Set("Authorization", "secret")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if strings.TrimSpace(w.Body.String()) != want {
			t.Fatalf("%d: %s", i, w.Body.String())
		}
	}
	if staticAssetCanUseS3("/sw.js") || staticAssetCanUseS3("/admin.webmanifest") || !staticAssetCanUseS3("/assets/app.js") {
		t.Fatal("service-worker shell policy")
	}
}

func TestStaticS3AssetsNeedNotExistInAppImage(t *testing.T) {
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/release/assets/app.js" {
			t.Error(r.URL.Path)
		}
		io.WriteString(w, "remote-only")
	}))
	defer storage.Close()
	handler := spaFallback(slog.Default(), http.NotFoundHandler(), t.TempDir(), nil, nil, nil, nil, nil, nil, "https://example.test", storage.URL+"/release")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/assets/app.js", nil))
	if w.Code != 200 || w.Body.String() != "remote-only" {
		t.Fatal(w.Code, w.Body.String())
	}
}
