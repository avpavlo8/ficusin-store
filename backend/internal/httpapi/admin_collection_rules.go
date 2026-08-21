package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/jackc/pgx/v5"
)

type collectionDefinitionRepository interface {
	ListCollectionDefinitions(context.Context) ([]admin.CollectionDefinition, error)
	CreateCollectionDefinition(context.Context, admin.Actor, admin.CollectionDefinitionInput) (admin.CollectionDefinition, error)
	UpdateCollectionDefinition(context.Context, admin.Actor, int64, admin.CollectionDefinitionInput) (admin.CollectionDefinition, error)
	DeleteCollectionDefinition(context.Context, admin.Actor, int64) error
}

func collectionDefinitionsHandler(adminAPI adminHandlers) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		_, actor, ok := adminAPI.authorize(response, request, admin.PermissionProductsRead)
		if !ok { return }
		provider, ok := adminAPI.repository.(collectionDefinitionRepository)
		if !ok { adminAPI.failed(response, "collection definitions unavailable", errors.New("collection definitions unavailable")); return }
		if request.Method == http.MethodGet {
			items, err := provider.ListCollectionDefinitions(request.Context())
			if err != nil { adminAPI.failed(response, "list collection definitions", err); return }
			writeJSON(response, http.StatusOK, map[string]any{"collections": items})
			return
		}
		if !admin.Can(actor.Role, admin.PermissionProductsEdit) {
			writeJSON(response, http.StatusForbidden, errorResponse{Error: "Недостаточно прав"})
			return
		}
		var input admin.CollectionDefinitionInput
		if decodeJSON(request, &input) != nil { writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректная подборка"}); return }
		item, err := provider.CreateCollectionDefinition(request.Context(), actor, input)
		if errors.Is(err, admin.ErrInvalidInput) { writeJSON(response, http.StatusBadRequest, errorResponse{Error: err.Error()}); return }
		if err != nil { adminAPI.failed(response, "create collection definition", err); return }
		writeJSON(response, http.StatusCreated, map[string]any{"collection": item})
	}
}

func collectionDefinitionHandler(adminAPI adminHandlers) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		_, actor, ok := adminAPI.authorize(response, request, admin.PermissionProductsEdit)
		if !ok { return }
		id, ok := pathID(response, request)
		if !ok { return }
		provider, ok := adminAPI.repository.(collectionDefinitionRepository)
		if !ok { adminAPI.failed(response, "collection definitions unavailable", errors.New("collection definitions unavailable")); return }
		if request.Method == http.MethodDelete {
			err := provider.DeleteCollectionDefinition(request.Context(), actor, id)
			if errors.Is(err, pgx.ErrNoRows) { writeJSON(response, http.StatusNotFound, errorResponse{Error: "Подборка не найдена"}); return }
			if err != nil { adminAPI.failed(response, "delete collection definition", err); return }
			writeJSON(response, http.StatusOK, map[string]bool{"deleted": true})
			return
		}
		var input admin.CollectionDefinitionInput
		if decodeJSON(request, &input) != nil { writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректная подборка"}); return }
		item, err := provider.UpdateCollectionDefinition(request.Context(), actor, id, input)
		if errors.Is(err, admin.ErrInvalidInput) { writeJSON(response, http.StatusBadRequest, errorResponse{Error: err.Error()}); return }
		if errors.Is(err, pgx.ErrNoRows) { writeJSON(response, http.StatusNotFound, errorResponse{Error: "Подборка не найдена"}); return }
		if err != nil { adminAPI.failed(response, "update collection definition", err); return }
		writeJSON(response, http.StatusOK, map[string]any{"collection": item})
	}
}
