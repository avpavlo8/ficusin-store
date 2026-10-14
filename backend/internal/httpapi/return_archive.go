package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
)

type returnArchiveRepository interface {
	ArchivedReturns(context.Context, admin.Actor) (admin.ReturnArchive, error)
	RestoreReturns(context.Context, admin.Actor, []int64, string, bool) error
	SetReturnPhotoArchived(context.Context, admin.Actor, int64, int64, bool) (admin.MarketplaceReturn, error)
}

func (h adminHandlers) archivedReturns(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := h.authorize(w, r, admin.PermissionReturnsDelete)
	if !ok {
		return
	}
	repo, ok := h.repository.(returnArchiveRepository)
	if !ok {
		writeJSON(w, 503, errorResponse{Error: "Удалённые возвраты недоступны"})
		return
	}
	result, err := repo.ArchivedReturns(r.Context(), actor)
	if err != nil {
		h.failed(w, "return archive", err)
		return
	}
	writeJSON(w, 200, result)
}
func (h adminHandlers) restoreReturns(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := h.authorize(w, r, admin.PermissionReturnsDelete)
	if !ok {
		return
	}
	repo, ok := h.repository.(returnArchiveRepository)
	if !ok {
		writeJSON(w, 503, errorResponse{Error: "Восстановление недоступно"})
		return
	}
	var input struct {
		IDs        []int64 `json:"ids"`
		DocumentID string  `json:"documentId"`
		All        bool    `json:"all"`
	}
	if decodeJSON(r, &input) != nil {
		writeJSON(w, 400, errorResponse{Error: "Выберите записи для восстановления"})
		return
	}
	if err := repo.RestoreReturns(r.Context(), actor, input.IDs, input.DocumentID, input.All); err != nil {
		writeJSON(w, 409, errorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (h adminHandlers) archiveReturnPhoto(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := h.authorize(w, r, admin.PermissionReturnsEdit)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	photoID, err := strconv.ParseInt(r.PathValue("photoID"), 10, 64)
	if err != nil || photoID <= 0 {
		writeJSON(w, 400, errorResponse{Error: "Некорректное фото"})
		return
	}
	repo, ok := h.repository.(returnArchiveRepository)
	if !ok {
		writeJSON(w, 503, errorResponse{Error: "Редактирование фото недоступно"})
		return
	}
	item, err := repo.SetReturnPhotoArchived(r.Context(), actor, id, photoID, !strings.HasSuffix(r.URL.Path, "/restore"))
	if err != nil {
		writeJSON(w, 409, errorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"item": item})
}
