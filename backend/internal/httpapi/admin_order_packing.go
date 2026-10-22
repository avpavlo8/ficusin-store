package httpapi

import (
	"context"
	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"net/http"
)

type adminOrderPackingRepository interface {
	OrderForPacking(context.Context, int64) (admin.Order, error)
	OrderPacking(context.Context, int64) (admin.OrderPackingState, error)
	QuoteOrderPacking(context.Context, admin.Actor, int64, admin.OrderPackingInput) (admin.ShipmentOfferQuote, error)
	FinalizeOrderPacking(context.Context, admin.Actor, int64, admin.OrderPackingInput) (admin.OrderPackingState, error)
}

func (handlers adminHandlers) orderPacking(response http.ResponseWriter, request *http.Request) {
	_, _, ok := handlers.authorize(response, request, admin.PermissionOrdersRead)
	if !ok {
		return
	}
	id, ok := pathID(response, request)
	if !ok {
		return
	}
	repository, ok := handlers.repository.(adminOrderPackingRepository)
	if !ok {
		writeJSON(response, http.StatusServiceUnavailable, errorResponse{Error: "Подготовка отправки недоступна"})
		return
	}
	state, err := repository.OrderPacking(request.Context(), id)
	if err != nil {
		handlers.failed(response, "load order packing", err)
		return
	}
	order, err := repository.OrderForPacking(request.Context(), id)
	if err != nil {
		handlers.failed(response, "load packed order", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"packing": state, "order": order})
}

func (handlers adminHandlers) quoteOrderPacking(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.authorize(response, request, admin.PermissionOrdersEdit)
	if !ok {
		return
	}
	id, ok := pathID(response, request)
	if !ok {
		return
	}
	repository, ok := handlers.repository.(adminOrderPackingRepository)
	if !ok {
		writeJSON(response, http.StatusServiceUnavailable, errorResponse{Error: "Подготовка отправки недоступна"})
		return
	}
	var input admin.OrderPackingInput
	if decodeJSON(request, &input) != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Проверьте упаковку заказа"})
		return
	}
	quote, err := repository.QuoteOrderPacking(request.Context(), actor, id, input)
	if err != nil {
		writeJSON(response, http.StatusConflict, errorResponse{Error: err.Error()})
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"quotes": quote.Quotes, "revision": input.Revision})
}

func (handlers adminHandlers) finalizeOrderPacking(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.authorize(response, request, admin.PermissionOrdersEdit)
	if !ok {
		return
	}
	id, ok := pathID(response, request)
	if !ok {
		return
	}
	repository, ok := handlers.repository.(adminOrderPackingRepository)
	if !ok {
		writeJSON(response, http.StatusServiceUnavailable, errorResponse{Error: "Подготовка отправки недоступна"})
		return
	}
	var input admin.OrderPackingInput
	if decodeJSON(request, &input) != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Проверьте упаковку заказа"})
		return
	}
	state, err := repository.FinalizeOrderPacking(request.Context(), actor, id, input)
	if err != nil {
		writeJSON(response, http.StatusConflict, errorResponse{Error: err.Error()})
		return
	}
	order, err := repository.OrderForPacking(request.Context(), id)
	if err != nil {
		handlers.failed(response, "load packed order", err)
		return
	}
	writeJSON(response, http.StatusAccepted, map[string]any{"packing": state, "order": order})
}
