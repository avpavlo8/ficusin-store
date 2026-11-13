package httpapi

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/saby"
)

func TestSabyCatalogRejectsFutureOrStaleCollectionWatermark(t *testing.T) {
	for _, tc := range []struct {
		name, observed string
		status         int
	}{
		{"legacy", "", http.StatusOK},
		{"collected before upload", time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano), http.StatusOK},
		{"future cannot release pending deductions", time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano), http.StatusBadRequest},
		{"stale is not a fresh stock import", time.Now().Add(-3 * time.Hour).UTC().Format(time.RFC3339Nano), http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"items":[{"id":278,"name":"fixture","cost":1000}]`
			if tc.observed != "" {
				body += fmt.Sprintf(`,"stockObservedAt":%q`, tc.observed)
			}
			request := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/saby/catalog", strings.NewReader(body+"}"))
			request.Header.Set("X-Ficusin-GitHub-OIDC", "token")
			response := httptest.NewRecorder()
			sabyCatalogSyncHandler(slog.New(slog.NewTextHandler(io.Discard, nil)), fakeSabySync{result: saby.Result{OK: true}}).ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", response.Code, tc.status, response.Body.String())
			}
		})
	}
}
