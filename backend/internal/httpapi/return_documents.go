package httpapi

import (
	"context"
	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"net/http"
)

type returnDocumentRepository interface {
	ReturnDocuments(context.Context, admin.Actor) ([]admin.ReturnIntake, error)
	CreateReturnReceiptBatch(context.Context, admin.Actor, []int64, bool) ([]admin.MarketplaceReturn, error)
	DeleteReturns(context.Context, admin.Actor, []int64, string, bool) error
	ReturnLabelItems(context.Context, admin.Actor, []int64) ([]admin.MarketplaceReturn, error)
}

func (h adminHandlers) documentRepository(w http.ResponseWriter, r *http.Request, permission string) (returnDocumentRepository, admin.Actor, bool) {
	_, actor, ok := h.authorize(w, r, permission)
	if !ok {
		return nil, actor, false
	}
	repo, ok := h.repository.(returnDocumentRepository)
	if !ok {
		writeJSON(w, 503, errorResponse{Error: "Документы возвратов недоступны"})
	}
	return repo, actor, ok
}
func (h adminHandlers) returnDocuments(w http.ResponseWriter, r *http.Request) {
	repo, actor, ok := h.documentRepository(w, r, admin.PermissionReturnsRead)
	if !ok {
		return
	}
	items, err := repo.ReturnDocuments(r.Context(), actor)
	if err != nil {
		writeJSON(w, 409, errorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}
func (h adminHandlers) returnReceiptBatch(w http.ResponseWriter, r *http.Request) {
	repo, actor, ok := h.documentRepository(w, r, admin.PermissionReturnsReceipt)
	if !ok {
		return
	}
	var input struct {
		IDs  []int64 `json:"ids"`
		Sell bool    `json:"sell"`
	}
	if decodeJSON(r, &input) != nil {
		writeJSON(w, 400, errorResponse{Error: "Выберите растения"})
		return
	}
	items, err := repo.CreateReturnReceiptBatch(r.Context(), actor, input.IDs, input.Sell)
	if err != nil {
		writeJSON(w, 409, errorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, 202, map[string]any{"items": items})
}
func (h adminHandlers) deleteReturns(w http.ResponseWriter, r *http.Request) {
	repo, actor, ok := h.documentRepository(w, r, admin.PermissionReturnsDelete)
	if !ok {
		return
	}
	var input struct {
		IDs        []int64 `json:"ids"`
		DocumentID string  `json:"documentId"`
		All        bool    `json:"all"`
	}
	if decodeJSON(r, &input) != nil {
		writeJSON(w, 400, errorResponse{Error: "Некорректное удаление"})
		return
	}
	if err := repo.DeleteReturns(r.Context(), actor, input.IDs, input.DocumentID, input.All); err != nil {
		writeJSON(w, 409, errorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
func (h adminHandlers) returnLabels(w http.ResponseWriter, r *http.Request) {
	repo, actor, ok := h.documentRepository(w, r, admin.PermissionReturnsRead)
	if !ok {
		return
	}
	var input struct {
		IDs         []int64 `json:"ids"`
		IncludeDate bool    `json:"includeDate"`
	}
	if decodeJSON(r, &input) != nil {
		writeJSON(w, 400, errorResponse{Error: "Выберите растения"})
		return
	}
	items, err := repo.ReturnLabelItems(r.Context(), actor, input.IDs)
	if err != nil {
		writeJSON(w, 409, errorResponse{Error: err.Error()})
		return
	}
	pdf, err := admin.BuildReturnLabels(items, input.IncludeDate)
	if err != nil {
		writeJSON(w, 500, errorResponse{Error: "Не удалось сформировать этикетки"})
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="recovery-labels.pdf"`)
	w.WriteHeader(200)
	_, _ = w.Write(pdf)
}
