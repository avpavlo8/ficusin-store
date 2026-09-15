package httpapi

import (
	"context"
	"encoding/base64"
	"net/http"
	"strconv"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
)

type marketplaceReturnRepository interface {
	MarketplaceReturns(context.Context, string, string, string) ([]admin.MarketplaceReturn, error)
	ReturnProducts(context.Context, string) ([]admin.ReturnProduct, error)
	CreateMarketplaceReturns(context.Context, admin.Actor, admin.ReturnCreate) ([]admin.MarketplaceReturn, error)
	UpdateMarketplaceReturn(context.Context, admin.Actor, int64, admin.ReturnUpdate) (admin.MarketplaceReturn, error)
	CreateReturnReceipt(context.Context, admin.Actor, int64) (admin.MarketplaceReturn, error)
	AddReturnPhoto(context.Context, admin.Actor, int64, string, string) (admin.MarketplaceReturn, error)
	ReturnPhoto(context.Context, int64) (string, []byte, error)
}

func (handlers adminHandlers) returnProducts(response http.ResponseWriter, request *http.Request) {
	_, _, ok := handlers.authorize(response, request, admin.PermissionReturnsRead)
	if !ok {
		return
	}
	repository, ok := handlers.returnRepository(response)
	if !ok {
		return
	}
	items, err := repository.ReturnProducts(request.Context(), request.URL.Query().Get("q"))
	if err != nil {
		handlers.failed(response, "return products", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}
func (handlers adminHandlers) returnRepository(response http.ResponseWriter) (marketplaceReturnRepository, bool) {
	repository, ok := handlers.repository.(marketplaceReturnRepository)
	if !ok {
		writeJSON(response, http.StatusServiceUnavailable, errorResponse{Error: "Журнал возвратов недоступен"})
	}
	return repository, ok
}
func (handlers adminHandlers) marketplaceReturns(response http.ResponseWriter, request *http.Request) {
	_, _, ok := handlers.authorize(response, request, admin.PermissionReturnsRead)
	if !ok {
		return
	}
	repository, ok := handlers.returnRepository(response)
	if !ok {
		return
	}
	items, err := repository.MarketplaceReturns(request.Context(), request.URL.Query().Get("status"), request.URL.Query().Get("channel"), request.URL.Query().Get("q"))
	if err != nil {
		handlers.failed(response, "marketplace returns", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}
func (handlers adminHandlers) createMarketplaceReturns(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.authorize(response, request, admin.PermissionReturnsEdit)
	if !ok {
		return
	}
	repository, ok := handlers.returnRepository(response)
	if !ok {
		return
	}
	var input admin.ReturnCreate
	if err := decodeJSON(request, &input); err != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректные данные возврата"})
		return
	}
	items, err := repository.CreateMarketplaceReturns(request.Context(), actor, input)
	if err != nil {
		writeJSON(response, http.StatusConflict, errorResponse{Error: err.Error()})
		return
	}
	writeJSON(response, http.StatusCreated, map[string]any{"items": items})
}
func (handlers adminHandlers) updateMarketplaceReturn(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.authorize(response, request, admin.PermissionReturnsEdit)
	if !ok {
		return
	}
	id, ok := pathID(response, request)
	if !ok {
		return
	}
	repository, ok := handlers.returnRepository(response)
	if !ok {
		return
	}
	var input admin.ReturnUpdate
	if err := decodeJSON(request, &input); err != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректное состояние"})
		return
	}
	item, err := repository.UpdateMarketplaceReturn(request.Context(), actor, id, input)
	if err != nil {
		writeJSON(response, http.StatusConflict, errorResponse{Error: err.Error()})
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"item": item})
}
func (handlers adminHandlers) createReturnReceipt(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.authorize(response, request, admin.PermissionReturnsReceipt)
	if !ok {
		return
	}
	id, ok := pathID(response, request)
	if !ok {
		return
	}
	repository, ok := handlers.returnRepository(response)
	if !ok {
		return
	}
	item, err := repository.CreateReturnReceipt(request.Context(), actor, id)
	if err != nil {
		writeJSON(response, http.StatusConflict, errorResponse{Error: err.Error()})
		return
	}
	writeJSON(response, http.StatusAccepted, map[string]any{"item": item})
}
func (handlers adminHandlers) addReturnPhoto(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.authorize(response, request, admin.PermissionReturnsEdit)
	if !ok {
		return
	}
	id, ok := pathID(response, request)
	if !ok {
		return
	}
	repository, ok := handlers.returnRepository(response)
	if !ok {
		return
	}
	var input struct {
		ContentType string `json:"contentType"`
		Data        string `json:"data"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректное фото"})
		return
	}
	if decoded, err := base64.StdEncoding.DecodeString(input.Data); err != nil || len(decoded) > 5<<20 {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Фото не должно превышать 5 МБ"})
		return
	}
	item, err := repository.AddReturnPhoto(request.Context(), actor, id, input.ContentType, input.Data)
	if err != nil {
		writeJSON(response, http.StatusConflict, errorResponse{Error: err.Error()})
		return
	}
	writeJSON(response, http.StatusCreated, map[string]any{"item": item})
}
func (handlers adminHandlers) returnPhoto(response http.ResponseWriter, request *http.Request) {
	_, _, ok := handlers.authorize(response, request, admin.PermissionReturnsRead)
	if !ok {
		return
	}
	id64, err := strconv.ParseInt(request.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(response, request)
		return
	}
	repository, ok := handlers.returnRepository(response)
	if !ok {
		return
	}
	contentType, data, err := repository.ReturnPhoto(request.Context(), id64)
	if err != nil {
		http.NotFound(response, request)
		return
	}
	response.Header().Set("Content-Type", contentType)
	response.Header().Set("Cache-Control", "private, max-age=300")
	_, _ = response.Write(data)
}
