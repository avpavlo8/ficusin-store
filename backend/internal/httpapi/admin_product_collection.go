package httpapi

import (
	"context"
	"errors"
	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strconv"
)

type productCollectionRepository interface {
	SetProductCollectionMembership(context.Context, admin.Actor, int64, int64, bool) error
}

func productCollectionHandler(adminAPI adminHandlers) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		_, actor, ok := adminAPI.authorize(response, request, admin.PermissionProductsEdit)
		if !ok {
			return
		}
		productID, ok := pathID(response, request)
		if !ok {
			return
		}
		collectionID, err := strconv.ParseInt(request.PathValue("collectionId"), 10, 64)
		var input struct {
			Included *bool `json:"included"`
		}
		if err != nil || collectionID <= 0 || decodeJSON(request, &input) != nil || input.Included == nil {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Укажите подборку и включение товара"})
			return
		}
		provider, ok := adminAPI.repository.(productCollectionRepository)
		if !ok {
			adminAPI.failed(response, "product collections unavailable", errors.New("product collections unavailable"))
			return
		}
		err = provider.SetProductCollectionMembership(request.Context(), actor, productID, collectionID, *input.Included)
		if errors.Is(err, admin.ErrInvalidInput) {
			writeJSON(response, 400, errorResponse{Error: err.Error()})
			return
		}
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(response, 404, errorResponse{Error: "Товар или подборка не найдены"})
			return
		}
		if err != nil {
			adminAPI.failed(response, "update product collection", err)
			return
		}
		writeJSON(response, 200, map[string]bool{"included": *input.Included})
	}
}
