package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/flowwow"
	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"net/http"
	"strconv"
	"strings"
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

func flowwowCandidatesHandler(h adminHandlers, s flowwowService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := h.authorize(w, r, admin.PermissionIntegrationsEdit); !ok {
			return
		}
		source, ok := s.(interface {
			Candidates(context.Context, string) ([]flowwow.Candidate, error)
		})
		if !ok {
			writeJSON(w, 503, errorResponse{Error: "Поиск недоступен"})
			return
		}
		items, e := source.Candidates(r.Context(), strings.TrimSpace(r.URL.Query().Get("q")))
		if e != nil {
			writeJSON(w, 400, errorResponse{Error: e.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"items": items})
	}
}
func flowwowLinkHandler(h adminHandlers, s flowwowService, apply bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, actor, ok := h.authorize(w, r, admin.PermissionIntegrationsEdit)
		if !ok {
			return
		}
		if !admin.Can(actor.Role, admin.PermissionProductsManage) {
			writeJSON(w, 403, errorResponse{Error: "Недостаточно прав для изменения связи"})
			return
		}
		var input admin.FlowwowLinkInput
		if decodeJSON(r, &input) != nil || input.VariantID <= 0 || input.RemoteID <= 0 {
			writeJSON(w, 400, errorResponse{Error: "Выберите карточку Flowwow и вариант сайта"})
			return
		}
		source, ok := s.(interface {
			Resolve(context.Context, int64) (integration.FlowwowProduct, string, error)
		})
		if !ok {
			writeJSON(w, 503, errorResponse{Error: "Проверка Flowwow недоступна"})
			return
		}
		repo, ok := h.repository.(interface {
			FlowwowLink(context.Context, admin.Actor, admin.FlowwowLinkInput, integration.FlowwowProduct, string, bool) (admin.FlowwowLinkView, error)
		})
		if !ok {
			writeJSON(w, 503, errorResponse{Error: "Связи недоступны"})
			return
		}
		remote, shop, e := source.Resolve(r.Context(), input.RemoteID)
		if e != nil {
			writeJSON(w, 400, errorResponse{Error: e.Error()})
			return
		}
		out, e := repo.FlowwowLink(r.Context(), actor, input, remote, shop, apply)
		if e != nil {
			if errors.Is(e, admin.ErrInvalidInput) {
				writeJSON(w, 409, errorResponse{Error: strings.TrimPrefix(e.Error(), admin.ErrInvalidInput.Error()+": ")})
			} else {
				h.failed(w, "Flowwow link", e)
			}
			return
		}
		writeJSON(w, 200, out)
	}
}
