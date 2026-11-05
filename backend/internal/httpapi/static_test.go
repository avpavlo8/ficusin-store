package httpapi

import (
	"net/http/httptest"
	"testing"
)

func TestManifestCachingAndContentType(t *testing.T) {
	for _, path := range []string{"/manifest.webmanifest", "/admin.webmanifest"} {
		response := httptest.NewRecorder()
		setStaticCaching(response, path)

		if got := response.Header().Get("Content-Type"); got != "application/manifest+json; charset=utf-8" {
			t.Fatalf("manifest Content-Type = %q", got)
		}
		if got := response.Header().Get("Cache-Control"); got != "public, max-age=3600" {
			t.Fatalf("manifest Cache-Control = %q", got)
		}
	}
}
