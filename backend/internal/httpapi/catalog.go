package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/avpavlo8/ficusin-store/backend/internal/catalog"
)

func catalogHandler(logger *slog.Logger, repository catalogRepository) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		products, err := repository.ListAvailable(request.Context())
		if err != nil {
			logger.Error("catalog request failed", "error", err)
			writeJSON(response, http.StatusServiceUnavailable, errorResponse{
				Error: "Не удалось загрузить каталог",
			})
			return
		}

		response.Header().Set("Cache-Control", "public, max-age=60, stale-while-revalidate=300")
		writeJSON(response, http.StatusOK, map[string]any{"products": products})
	})
}

func productDetailHandler(logger *slog.Logger, repository catalogRepository) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		detail, err := repository.DetailBySlug(request.Context(), request.PathValue("slug"))
		if errors.Is(err, catalog.ErrNotFound) {
			writeJSON(response, http.StatusNotFound, errorResponse{Error: "Товар не найден"})
			return
		}
		if err != nil {
			logger.Error("product detail failed", "error", err)
			writeJSON(response, http.StatusServiceUnavailable, errorResponse{Error: "Не удалось загрузить товар"})
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"product": detail})
	})
}
