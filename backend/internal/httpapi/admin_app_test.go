package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdminAppInitialHTML(t *testing.T) {
	shell, err := os.ReadFile("../../../frontend/index.html")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), shell, 0600); err != nil {
		t.Fatal(err)
	}
	handler := spaFallback(slog.New(slog.NewTextHandler(io.Discard, nil)), http.NotFoundHandler(), dir, nil, nil, nil, nil, nil, nil, "https://ficusin.ru")
	for _, target := range []string{"/admin", "/admin/", "/admin?section=orders", "/admin/orders", "/login?returnTo=%2Fadmin%3Fsection%3Dprocurement", "/", "/login", "/login?returnTo=%2Fadministrator", "/login?returnTo=%2F%2Fevil.example%2Fadmin"} {
		t.Run(target, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, target, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			body := response.Body.String()
			admin := strings.HasPrefix(target, "/admin") || strings.Contains(target, "returnTo=%2Fadmin%3F")
			if admin {
				for _, want := range []string{"/admin.webmanifest?v=admin-v1", "/assets/brand/admin-v1/app-180.png", "<title>Фикусин — управление</title>", `content="noindex,nofollow"`} {
					if !strings.Contains(body, want) {
						t.Errorf("initial HTML missing %q", want)
					}
				}
				if strings.Contains(body, "/manifest.webmanifest?") {
					t.Error("admin loads storefront manifest")
				}
			} else if !strings.Contains(body, "/manifest.webmanifest?v=app-logo-v2") || strings.Contains(body, "/admin.webmanifest") {
				t.Error("storefront identity changed outside admin")
			}
		})
	}
}
