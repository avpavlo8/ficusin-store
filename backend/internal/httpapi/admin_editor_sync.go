package httpapi

import (
	"context"
	"net/http"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
)

func editorSyncExistingHandler(h adminHandlers) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, actor, ok := h.authorize(w, r, admin.PermissionProductsEdit)
		if !ok {
			return
		}
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		// This command has no client-controlled configuration or identity.
		if decodeJSONWithLimit(r, &struct{}{}, 1024) != nil {
			writeJSON(w, 400, errorResponse{Error: "Настройки площадок в этом запросе не принимаются"})
			return
		}
		repo, ok := h.repository.(interface {
			SyncExistingEditorPlan(context.Context, admin.Actor, int64) (admin.EditorSyncResult, error)
		})
		if !ok {
			writeJSON(w, 503, errorResponse{Error: "Обновление площадок недоступно"})
			return
		}
		result, err := repo.SyncExistingEditorPlan(r.Context(), actor, id)
		if err != nil {
			h.failed(w, "editor sync existing", err)
			return
		}
		writeJSON(w, 200, result)
	}
}
