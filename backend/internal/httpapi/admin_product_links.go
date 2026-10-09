package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
)

type productLinksRepository interface {
	ListProductLinks(context.Context) ([]admin.ProductLinks, error)
}

func productLinksHandler(adminAPI adminHandlers) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		_, _, ok := adminAPI.authorize(response, request, admin.PermissionProductsRead)
		if !ok {
			return
		}
		provider, ok := adminAPI.repository.(productLinksRepository)
		if !ok {
			adminAPI.failed(response, "product links unavailable", errors.New("product links unavailable"))
			return
		}
		products, err := provider.ListProductLinks(request.Context())
		if err != nil {
			adminAPI.failed(response, "list product links", err)
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"products": products})
	}
}
