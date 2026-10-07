package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/procurement"
)

type namedDraftService interface {
	ListNamedPlanDrafts(context.Context, procurement.Actor) ([]procurement.NamedPlanDraft, error)
	LoadNamedPlanDraft(context.Context, procurement.Actor, int64) (procurement.NamedPlanDraft, error)
	CreateNamedPlanDraft(context.Context, procurement.Actor, string, json.RawMessage) (procurement.NamedPlanDraft, error)
	UpdateNamedPlanDraft(context.Context, procurement.Actor, int64, string, json.RawMessage) (procurement.NamedPlanDraft, error)
	DeleteNamedPlanDraft(context.Context, procurement.Actor, int64) error
}

func (handlers procurementHandlers) namedDraftService(response http.ResponseWriter) (namedDraftService, bool) {
	service, ok := handlers.service.(namedDraftService)
	if !ok {
		writeJSON(response, http.StatusServiceUnavailable, errorResponse{Error: "Черновики пока недоступны"})
	}
	return service, ok
}

func (handlers procurementHandlers) listNamedPlanDrafts(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := handlers.admin.authorize(w, r, admin.PermissionProcurementRead)
	if !ok {
		return
	}
	service, ok := handlers.namedDraftService(w)
	if !ok {
		return
	}
	drafts, err := service.ListNamedPlanDrafts(r.Context(), procurement.Actor{CustomerID: actor.CustomerID, Role: actor.Role})
	if err != nil {
		handlers.failed(w, "list procurement drafts", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"drafts": drafts})
}

func (handlers procurementHandlers) loadNamedPlanDraft(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := handlers.admin.authorize(w, r, admin.PermissionProcurementRead)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	service, ok := handlers.namedDraftService(w)
	if !ok {
		return
	}
	draft, err := service.LoadNamedPlanDraft(r.Context(), procurement.Actor{CustomerID: actor.CustomerID, Role: actor.Role}, id)
	if err != nil {
		handlers.failed(w, "load procurement draft", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"draft": draft})
}

func decodeNamedDraft(w http.ResponseWriter, r *http.Request) (string, json.RawMessage, bool) {
	var input struct {
		Title   string          `json:"title"`
		Payload json.RawMessage `json:"payload"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if decodeJSON(r, &input) != nil || len(input.Payload) == 0 || !json.Valid(input.Payload) {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "Некорректный черновик"})
		return "", nil, false
	}
	return input.Title, input.Payload, true
}

func (handlers procurementHandlers) createNamedPlanDraft(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := handlers.admin.authorize(w, r, admin.PermissionProcurementEdit)
	if !ok {
		return
	}
	service, ok := handlers.namedDraftService(w)
	if !ok {
		return
	}
	title, payload, ok := decodeNamedDraft(w, r)
	if !ok {
		return
	}
	draft, err := service.CreateNamedPlanDraft(r.Context(), procurement.Actor{CustomerID: actor.CustomerID, Role: actor.Role}, title, payload)
	if err != nil {
		handlers.failed(w, "create procurement draft", err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"draft": draft})
}

func (handlers procurementHandlers) updateNamedPlanDraft(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := handlers.admin.authorize(w, r, admin.PermissionProcurementEdit)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	service, ok := handlers.namedDraftService(w)
	if !ok {
		return
	}
	title, payload, ok := decodeNamedDraft(w, r)
	if !ok {
		return
	}
	draft, err := service.UpdateNamedPlanDraft(r.Context(), procurement.Actor{CustomerID: actor.CustomerID, Role: actor.Role}, id, title, payload)
	if err != nil {
		handlers.failed(w, "update procurement draft", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"draft": draft})
}

func (handlers procurementHandlers) deleteNamedPlanDraft(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := handlers.admin.authorize(w, r, admin.PermissionProcurementEdit)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	service, ok := handlers.namedDraftService(w)
	if !ok {
		return
	}
	if err := service.DeleteNamedPlanDraft(r.Context(), procurement.Actor{CustomerID: actor.CustomerID, Role: actor.Role}, id); err != nil {
		handlers.failed(w, "delete procurement draft", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
