package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/avpavlo8/ficusin-store/backend/internal/saby"
)

type sabySyncService interface {
	Verify(context.Context, string) error
	Sync(context.Context, []saby.CatalogItem) (saby.Result, error)
	SyncSales(context.Context, saby.SalesUpload) (saby.SalesResult, error)
}

func sabyCatalogSyncHandler(logger *slog.Logger, service sabySyncService) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		token := strings.TrimSpace(request.Header.Get("X-Ficusin-GitHub-OIDC"))
		if token == "" {
			writeJSON(response, http.StatusUnauthorized, errorResponse{Error: "Доступ запрещён"})
			return
		}
		if err := service.Verify(request.Context(), token); err != nil {
			code := "auth-error"
			var authError *saby.AuthError
			if errors.As(err, &authError) {
				code = authError.Code
			}
			logger.Warn("Saby synchronization rejected", "code", code, "error", err)
			response.Header().Set("X-Saby-Sync-Error", code)
			writeJSON(response, http.StatusForbidden, map[string]string{
				"error": "Доступ запрещён", "code": code,
			})
			return
		}

		var body struct {
			Items []saby.CatalogItem `json:"items"`
		}
		if err := decodeJSONWithLimit(request, &body, 16<<20); err != nil ||
			body.Items == nil || len(body.Items) > 2000 {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректный каталог"})
			return
		}
		result, err := service.Sync(request.Context(), body.Items)
		if err != nil {
			if err.Error() == "empty Saby catalog" {
				writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Каталог Saby пуст"})
				return
			}
			logger.Error("Saby synchronization failed", "error", err)
			response.Header().Set("X-Saby-Sync-Error", sabySyncErrorCode(err))
			writeJSON(response, http.StatusInternalServerError, errorResponse{
				Error: "Не удалось обновить каталог",
			})
			return
		}
		writeJSON(response, http.StatusOK, result)
	})
}

func sabySyncErrorCode(err error) string {
	message := err.Error()
	for _, candidate := range []struct{ prefix, code string }{
		{"unsafe Saby catalog", "catalog-health"},
		{"read previous Saby catalogue health", "catalog-health-read"},
		{"start Saby sync", "sync-run-start"},
		{"begin Saby sync", "sync-transaction"},
		{"upsert Saby warehouse", "warehouse-upsert"},
		{"pack Saby catalogue", "catalog-pack"},
		{"upsert Saby nomenclature", "catalog-upsert"},
		{"map Saby characteristics", "characteristics-map"},
		{"map Saby IDs", "ids-map"},
		{"map Saby codes", "codes-map"},
		{"mark missing Saby items", "catalog-missing"},
		{"update Saby stock", "stock-update"},
		{"update Saby names", "names-update"},
		{"update Saby descriptions", "descriptions-update"},
		{"update Saby prices", "prices-update"},
		{"query Saby photo targets", "photos-query"},
		{"scan Saby photo target", "photos-scan"},
		{"read Saby photo targets", "photos-read"},
		{"replace Saby media", "photos-replace"},
		{"insert Saby media", "photos-insert"},
		{"commit Saby sync", "sync-commit"},
		{"finish Saby sync", "sync-run-finish"},
	} {
		if strings.HasPrefix(message, candidate.prefix) {
			return candidate.code
		}
	}
	return "store-error"
}

func sabySalesSyncHandler(logger *slog.Logger, service sabySyncService) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		token := strings.TrimSpace(request.Header.Get("X-Ficusin-GitHub-OIDC"))
		if token == "" {
			writeJSON(response, http.StatusUnauthorized, errorResponse{Error: "Доступ запрещён"})
			return
		}
		if err := service.Verify(request.Context(), token); err != nil {
			logger.Warn("Saby sales synchronization rejected", "error", err)
			writeJSON(response, http.StatusForbidden, errorResponse{Error: "Доступ запрещён"})
			return
		}
		var body saby.SalesUpload
		if err := decodeJSONWithLimit(request, &body, 32<<20); err != nil || body.Items == nil {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректная история продаж"})
			return
		}
		result, err := service.SyncSales(request.Context(), body)
		if err != nil {
			if strings.HasPrefix(err.Error(), "invalid Saby sales") {
				writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректная история продаж"})
				return
			}
			logger.Error("Saby sales synchronization failed", "error", err)
			writeJSON(response, http.StatusInternalServerError, errorResponse{Error: "Не удалось обновить продажи"})
			return
		}
		writeJSON(response, http.StatusOK, result)
	})
}
