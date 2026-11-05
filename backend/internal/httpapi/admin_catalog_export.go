package httpapi

import (
	"context"
	"errors"
	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/catalogsync"
	"net/http"
)

type catalogExportService interface {
	View(context.Context, int64) (catalogsync.View, error)
	Enqueue(context.Context, int64, int64, string, bool) (catalogsync.View, error)
}

func catalogExportHandler(handlers adminHandlers, service catalogExportService, write bool) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		permission := admin.PermissionProductsRead
		if write {
			permission = admin.PermissionProductsManage
		}
		_, actor, ok := handlers.authorize(response, request, permission)
		if !ok {
			return
		}
		if actor.Role != admin.RoleOwner {
			writeJSON(response, http.StatusForbidden, errorResponse{Error: "Только владелец может отправлять каталог в СБИС"})
			return
		}
		if service == nil {
			writeJSON(response, http.StatusServiceUnavailable, errorResponse{Error: "Отправка каталога в СБИС недоступна"})
			return
		}
		id, ok := pimPathID(response, request, "variantId")
		if !ok {
			return
		}
		var result catalogsync.View
		var err error
		if write {
			var input struct {
				Fingerprint string `json:"fingerprint"`
				CheckOnly   bool   `json:"checkOnly"`
			}
			if decodeJSON(request, &input) != nil {
				writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Проверьте данные отправки"})
				return
			}
			result, err = service.Enqueue(request.Context(), id, actor.CustomerID, input.Fingerprint, input.CheckOnly)
		} else {
			result, err = service.View(request.Context(), id)
		}
		if err != nil {
			switch {
			case errors.Is(err, catalogsync.ErrNotFound):
				writeJSON(response, http.StatusNotFound, errorResponse{Error: err.Error()})
			case errors.Is(err, catalogsync.ErrInvalid):
				writeJSON(response, http.StatusBadRequest, errorResponse{Error: err.Error()})
			case errors.Is(err, catalogsync.ErrConflict):
				writeJSON(response, http.StatusConflict, errorResponse{Error: err.Error()})
			default:
				handlers.failed(response, "catalogue Saby export", err)
			}
			return
		}
		status := http.StatusOK
		if write && result.State != "synced" {
			status = http.StatusAccepted
		}
		writeJSON(response, status, result)
	}
}
