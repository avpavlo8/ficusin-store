package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/avpavlo8/ficusin-store/backend/internal/catalog"
)

func catalogHandler(logger *slog.Logger, repository catalogRepository, cache *publicJSONCache) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		entry, hit, loadDuration, encodeDuration, err := cache.get(request.Context(), "catalog", func(ctx context.Context) (any, error) {
			products, loadErr := repository.ListAvailable(ctx)
			return map[string]any{"products": products}, loadErr
		})
		if err != nil {
			logger.Error("catalog request failed", "error", err)
			writeJSON(response, http.StatusServiceUnavailable, errorResponse{
				Error: "Не удалось загрузить каталог",
			})
			return
		}

		writePublicCacheResponse(response, request, entry, hit, loadDuration, encodeDuration)
	})
}

func categoriesHandler(logger *slog.Logger, repository catalogRepository, cache *publicJSONCache) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		entry, hit, loadDuration, encodeDuration, err := cache.get(request.Context(), "categories", func(ctx context.Context) (any, error) {
			items, loadErr := repository.ListCategories(ctx)
			return map[string]any{"categories": items}, loadErr
		})
		if err != nil {
			logger.Error("categories request failed", "error", err)
			writeJSON(response, http.StatusServiceUnavailable, errorResponse{Error: "Не удалось загрузить категории"})
			return
		}
		writePublicCacheResponse(response, request, entry, hit, loadDuration, encodeDuration)
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
