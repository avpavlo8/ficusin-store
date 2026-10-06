package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/procurement"
)

type procurementDraftService interface {
	LoadPlanDraft(context.Context, procurement.Actor) (*procurement.PlanDraft, error)
	SavePlanDraft(context.Context, procurement.Actor, json.RawMessage) (procurement.PlanDraft, error)
	DeletePlanDraft(context.Context, procurement.Actor) error
}

func (handlers procurementHandlers) draftService(response http.ResponseWriter) (procurementDraftService, bool) {
	service, ok := handlers.service.(procurementDraftService)
	if !ok {
		writeJSON(response, http.StatusServiceUnavailable, errorResponse{Error: "Черновики пока недоступны"})
	}
	return service, ok
}

func (handlers procurementHandlers) loadPlanDraft(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.admin.authorize(response, request, admin.PermissionProcurementRead)
	if !ok {
		return
	}
	service, ok := handlers.draftService(response)
	if !ok {
		return
	}
	draft, err := service.LoadPlanDraft(request.Context(), procurement.Actor{CustomerID: actor.CustomerID, Role: actor.Role})
	if err != nil {
		handlers.failed(response, "load procurement draft", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"draft": draft})
}

func (handlers procurementHandlers) savePlanDraft(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.admin.authorize(response, request, admin.PermissionProcurementEdit)
	if !ok {
		return
	}
	service, ok := handlers.draftService(response)
	if !ok {
		return
	}
	var input struct {
		Payload json.RawMessage `json:"payload"`
	}
	request.Body = http.MaxBytesReader(response, request.Body, 1<<20)
	if decodeJSON(request, &input) != nil || len(input.Payload) == 0 || !json.Valid(input.Payload) {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректный черновик"})
		return
	}
	draft, err := service.SavePlanDraft(request.Context(), procurement.Actor{CustomerID: actor.CustomerID, Role: actor.Role}, input.Payload)
	if err != nil {
		handlers.failed(response, "save procurement draft", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"draft": draft})
}

func (handlers procurementHandlers) deletePlanDraft(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.admin.authorize(response, request, admin.PermissionProcurementEdit)
	if !ok {
		return
	}
	service, ok := handlers.draftService(response)
	if !ok {
		return
	}
	if err := service.DeletePlanDraft(request.Context(), procurement.Actor{CustomerID: actor.CustomerID, Role: actor.Role}); err != nil {
		handlers.failed(response, "delete procurement draft", err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}
