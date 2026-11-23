package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
)

type adminOrderCDEKRepository interface {
	OrderCDEK(context.Context, int64) (admin.OrderCDEKState, error)
	UpdateOrderCDEK(context.Context, admin.Actor, int64, admin.OrderCDEKInput) (admin.OrderCDEKState, error)
	OrderForPacking(context.Context, int64) (admin.Order, error)
}

func (handlers adminHandlers) orderCDEK(response http.ResponseWriter, request *http.Request) {
	_, _, ok := handlers.authorize(response, request, admin.PermissionOrdersRead)
	if !ok {
		return
	}
	id, ok := pathID(response, request)
	if !ok {
		return
	}
	repository, ok := handlers.repository.(adminOrderCDEKRepository)
	if !ok {
		writeJSON(response, http.StatusServiceUnavailable, errorResponse{Error: "Данные СДЭК недоступны"})
		return
	}
	state, err := repository.OrderCDEK(request.Context(), id)
	if err != nil {
		handlers.failed(response, "load CDEK registration", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"shipping": state})
}

func (handlers adminHandlers) updateOrderCDEK(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.authorize(response, request, admin.PermissionOrdersEdit)
	if !ok {
		return
	}
	id, ok := pathID(response, request)
	if !ok {
		return
	}
	repository, ok := handlers.repository.(adminOrderCDEKRepository)
	if !ok {
		writeJSON(response, http.StatusServiceUnavailable, errorResponse{Error: "Изменение отправления СДЭК недоступно"})
		return
	}
	var input admin.OrderCDEKInput
	if decodeJSON(request, &input) != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: admin.ErrOrderCDEKInput.Error()})
		return
	}
	state, err := repository.UpdateOrderCDEK(request.Context(), actor, id, input)
	switch {
	case errors.Is(err, admin.ErrOrderCDEKInput):
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	case errors.Is(err, admin.ErrOrderCDEKConflict):
		writeJSON(response, http.StatusConflict, errorResponse{Error: err.Error()})
		return
	case err != nil:
		handlers.failed(response, "update CDEK registration", err)
		return
	}
	order, err := repository.OrderForPacking(request.Context(), id)
	if err != nil {
		handlers.failed(response, "load order after CDEK update", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"shipping": state, "order": order})
}
