package httpapi

import (
	"context"
	"encoding/json"
	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"net/http"
	"strconv"
)

type flowwowService interface {
	List(context.Context) (any, error)
	Reference(context.Context, string, string) (json.RawMessage, error)
	Remote(context.Context, int) (any, error)
}

func flowwowHandler(h adminHandlers, s flowwowService, kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		permission := admin.PermissionIntegrationsEdit
		if kind == "references" {
			permission = admin.PermissionProductsRead
		}
		if _, _, ok := h.authorize(w, r, permission); !ok {
			return
		}
		var data any
		var e error
		switch kind {
		case "list":
			data, e = s.List(r.Context())
		case "references":
			data, e = s.Reference(r.Context(), r.URL.Query().Get("kind"), r.URL.Query().Get("category"))
		case "products":
			page, err := strconv.Atoi(r.URL.Query().Get("page"))
			if err != nil || page < 0 {
				writeJSON(w, 400, errorResponse{Error: "Укажите номер страницы от 0"})
				return
			}
			data, e = s.Remote(r.Context(), page)
		}
		if e != nil {
			writeJSON(w, 503, errorResponse{Error: e.Error()})
			return
		}
		writeJSON(w, 200, data)
	}
}
