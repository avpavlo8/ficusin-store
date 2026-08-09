package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/avpavlo8/ficusin-store/backend/internal/saby"
)

type sabySyncService interface {
	Verify(context.Context, string) error
	Sync(context.Context, []saby.CatalogItem) (saby.Result, error)
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
			// Sample — одна позиция выгрузки целиком, полями и значениями.
			// Нужна ровно для одного: найти, в каком поле СБИС держит код
			// товара, который менеджер видит глазами. Уберём, как найдём.
			Sample map[string]string `json:"sample"`
		}
		if err := decodeJSONWithLimit(request, &body, 16<<20); err != nil ||
			body.Items == nil || len(body.Items) > 2000 {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректный каталог"})
			return
		}
		if len(body.Sample) > 0 {
			// Панель хостинга показывает только текст сообщения, поэтому
			// поля перечисляем прямо в нём.
			names := make([]string, 0, len(body.Sample))
			for name, value := range body.Sample {
				names = append(names, name+"="+value)
			}
			sort.Strings(names)
			logger.Info("поля выгрузки СБИС: " + strings.Join(names, " | "))
		}
		result, err := service.Sync(request.Context(), body.Items)
		if err != nil {
			if err.Error() == "empty Saby catalog" {
				writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Каталог Saby пуст"})
				return
			}
			logger.Error("Saby synchronization failed", "error", err)
			writeJSON(response, http.StatusInternalServerError, errorResponse{
				Error: "Не удалось обновить каталог",
			})
			return
		}
		writeJSON(response, http.StatusOK, result)
	})
}
