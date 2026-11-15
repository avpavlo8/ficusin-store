package httpapi

import (
	"context"
	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"net/http"
	"strconv"
)

func (handlers adminHandlers) removeProduct(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.authorize(response, request, admin.PermissionDelete)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(request.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректный товар"})
		return
	}
	remover, ok := handlers.repository.(interface {
		RemoveProduct(context.Context, admin.Actor, int64) (admin.ProductRemoval, error)
	})
	if !ok {
		writeJSON(response, http.StatusNotImplemented, errorResponse{Error: "Удаление карточки недоступно"})
		return
	}
	result, err := remover.RemoveProduct(request.Context(), actor, id)
	if err != nil {
		handlers.failed(response, "remove admin product", err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}
