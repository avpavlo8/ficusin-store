package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"net/http"
	"strconv"
	"time"
)

type editorPlanRepository interface {
	ReadEditorPlan(context.Context, int64) (admin.EditorPlanView, error)
	SaveEditorPlan(context.Context, admin.Actor, int64, admin.EditorPlan, bool) (admin.EditorPlanView, error)
	CachedEditorReference(context.Context, string, string, string) (json.RawMessage, error)
	CacheEditorReference(context.Context, string, string, string, json.RawMessage) error
	SabyEditorReferences(context.Context) (map[string]any, error)
}
type editorReferenceSource interface {
	ReadEditorReference(context.Context, string, string, int64, int64, int64, string, ...string) (json.RawMessage, error)
}

func editorPlanHandler(h adminHandlers) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := admin.PermissionProductsRead
		if r.Method == http.MethodPut {
			p = admin.PermissionProductsManage
		}
		_, actor, ok := h.authorize(w, r, p)
		if !ok {
			return
		}
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		repo, ok := h.repository.(editorPlanRepository)
		if !ok {
			writeJSON(w, 503, errorResponse{Error: "Настройки площадок недоступны"})
			return
		}
		var v admin.EditorPlanView
		var err error
		if r.Method == http.MethodGet {
			v, err = repo.ReadEditorPlan(r.Context(), id)
		} else {
			var in struct {
				Config admin.EditorPlan `json:"config"`
				Submit bool             `json:"submit"`
			}
			if decodeJSON(r, &in) != nil {
				writeJSON(w, 400, errorResponse{Error: "Проверьте настройки площадок"})
				return
			}
			v, err = repo.SaveEditorPlan(r.Context(), actor, id, in.Config, in.Submit)
		}
		if err != nil {
			h.failed(w, "editor plan", err)
			return
		}
		writeJSON(w, 200, v)
	}
}
func editorReferencesHandler(h adminHandlers, source editorReferenceSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := h.authorize(w, r, admin.PermissionProductsRead); !ok {
			return
		}
		repo, ok := h.repository.(editorPlanRepository)
		if !ok {
			writeJSON(w, 503, errorResponse{Error: "Справочники недоступны"})
			return
		}
		q := r.URL.Query()
		ch, kind := q.Get("channel"), q.Get("kind")
		if ch == "saby" {
			v, e := repo.SabyEditorReferences(r.Context())
			if e != nil {
				h.failed(w, "Saby references", e)
				return
			}
			writeJSON(w, 200, v)
			return
		}
		if kind == "existing" {
			if source == nil {
				writeJSON(w, 503, errorResponse{Error: "Проверка площадки не подключена"})
				return
			}
			if (ch != "ozon" && ch != "wb") || len(q.Get("q")) > 128 || q.Get("q") == "" {
				writeJSON(w, 400, errorResponse{Error: "Укажите артикул товара"})
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
			defer cancel()
			raw, e := source.ReadEditorReference(ctx, ch, kind, 0, 0, 0, q.Get("q"))
			if e != nil {
				writeJSON(w, 400, errorResponse{Error: e.Error()})
				return
			}
			writeJSON(w, 200, map[string]any{"data": raw})
			return
		}
		a, _ := strconv.ParseInt(q.Get("category"), 10, 64)
		b, _ := strconv.ParseInt(q.Get("typeId"), 10, 64)
		d, _ := strconv.ParseInt(q.Get("attribute"), 10, 64)
		scope := fmt.Sprintf("%d:%d:%d:%s", a, b, d, q.Get("q"))
		if q.Get("cursor") != "" {
			scope += ":cursor=" + q.Get("cursor")
		}
		raw, e := repo.CachedEditorReference(r.Context(), ch, kind, scope)
		if e != nil {
			h.failed(w, "reference cache", e)
			return
		}
		if len(raw) == 0 {
			if source == nil {
				writeJSON(w, 503, errorResponse{Error: "Справочник не подключён"})
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
			defer cancel()
			raw, e = source.ReadEditorReference(ctx, ch, kind, a, b, d, q.Get("q"), q.Get("cursor"))
			if e != nil {
				writeJSON(w, 503, errorResponse{Error: e.Error()})
				return
			}
			if e = repo.CacheEditorReference(ctx, ch, kind, scope, raw); e != nil {
				h.failed(w, "save reference cache", e)
				return
			}
		}
		writeJSON(w, 200, map[string]any{"data": raw})
	}
}

func editorPublicationRetryHandler(h adminHandlers) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, actor, ok := h.authorize(w, r, admin.PermissionProductsManage)
		if !ok {
			return
		}
		product, ok := pathID(w, r)
		if !ok {
			return
		}
		var in struct {
			VariantID int64 `json:"variantId"`
		}
		if decodeJSON(r, &in) != nil || in.VariantID <= 0 {
			writeJSON(w, 400, errorResponse{Error: "Укажите вариант товара"})
			return
		}
		repo, ok := h.repository.(interface {
			RetryEditorPublication(context.Context, admin.Actor, int64, int64, string) error
		})
		if !ok {
			writeJSON(w, 503, errorResponse{Error: "Повторная проверка недоступна"})
			return
		}
		if e := repo.RetryEditorPublication(r.Context(), actor, product, in.VariantID, r.PathValue("channel")); e != nil {
			h.failed(w, "editor publication retry", e)
			return
		}
		writeJSON(w, 200, map[string]bool{"queued": true})
	}
}
