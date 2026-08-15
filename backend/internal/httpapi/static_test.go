package httpapi

import (
	"net/http/httptest"
	"testing"
)

func TestManifestCachingAndContentType(t *testing.T) {
	response := httptest.NewRecorder()
	setStaticCaching(response, "/manifest.webmanifest")

	if got := response.Header().Get("Content-Type"); got != "application/manifest+json; charset=utf-8" {
		t.Fatalf("manifest Content-Type = %q", got)
	}
	if got := response.Header().Get("Cache-Control"); got != "public, max-age=3600" {
		t.Fatalf("manifest Cache-Control = %q", got)
	}
}
