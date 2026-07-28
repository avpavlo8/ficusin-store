package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/saby"
)

type fakeSabySync struct {
	verifyErr error
	result    saby.Result
	syncErr   error
}

func (service fakeSabySync) Verify(context.Context, string) error {
	return service.verifyErr
}

func (service fakeSabySync) Sync(
	_ context.Context,
	_ []saby.CatalogItem,
) (saby.Result, error) {
	return service.result, service.syncErr
}

func TestSabyCatalogSyncRequiresOIDCToken(t *testing.T) {
	t.Parallel()
	handler := sabyCatalogSyncHandler(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		fakeSabySync{},
	)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/saby/catalog", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestSabyCatalogSyncReturnsSafeAuthCode(t *testing.T) {
	t.Parallel()
	handler := sabyCatalogSyncHandler(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		fakeSabySync{verifyErr: &saby.AuthError{
			Code: "jwt-claims", Err: errors.New("private details"),
		}},
	)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/saby/catalog", nil)
	request.Header.Set("X-Ficusin-GitHub-OIDC", "token")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d", response.Code)
	}
	if got := response.Header().Get("X-Saby-Sync-Error"); got != "jwt-claims" {
		t.Fatalf("X-Saby-Sync-Error = %q", got)
	}
}

func TestSabyCatalogSyncAcceptsCatalog(t *testing.T) {
	t.Parallel()
	handler := sabyCatalogSyncHandler(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		fakeSabySync{result: saby.Result{
			OK: true, ItemsRead: 1, SyncedAt: "2026-07-28T12:00:00Z",
		}},
	)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/integrations/saby/catalog",
		strings.NewReader(`{"items":[{"id":278,"name":"Фикус","cost":1000}]}`),
	)
	request.Header.Set("X-Ficusin-GitHub-OIDC", "token")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"itemsRead":1`) {
		t.Fatalf("body = %s", response.Body.String())
	}
}
