package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/avpavlo8/ficusin-store/backend/internal/catalog"
)

type collectionRepository interface {
	ListCollections(context.Context) ([]catalog.Collection, error)
}

// collectionsHandler feeds the tabs above the storefront grid. A failure
// here costs the tabs, not the shop: the grid still shows everything.
func collectionsHandler(logger *slog.Logger, repository collectionRepository) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if repository == nil {
			writeJSON(response, http.StatusOK, map[string]any{"collections": []catalog.Collection{}})
			return
		}
		collections, err := repository.ListCollections(request.Context())
		if err != nil {
			logger.Error("list collections failed", "error", err)
			writeJSON(response, http.StatusOK, map[string]any{"collections": []catalog.Collection{}})
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"collections": collections})
	})
}
