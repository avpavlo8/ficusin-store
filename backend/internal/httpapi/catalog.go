package httpapi

import (
	"log/slog"
	"net/http"
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
