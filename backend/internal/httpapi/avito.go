package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/avito"
)

type avitoService interface {
	Configured() bool
	Import(context.Context) (int, error)
	List(context.Context) ([]avito.Listing, avito.State, error)
	SearchProducts(context.Context, string) ([]avito.Product, error)
	ReplaceProducts(context.Context, string, []int64) error
	SetPublication(context.Context, bool) error
	WriteFeed(context.Context, string, http.ResponseWriter) error
}

func avitoFeedHandler(service avitoService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimSuffix(r.PathValue("token"), ".xml")
		if token == r.PathValue("token") || token == "" {
			http.NotFound(w, r)
			return
		}
		if err := service.WriteFeed(r.Context(), token, w); err != nil {
			http.NotFound(w, r)
		}
	}
}

func avitoListHandler(administration adminHandlers, service avitoService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := administration.authorize(w, r, admin.PermissionProcurementRead); !ok {
			return
		}
		items, state, err := service.List(r.Context())
		if err != nil {
			administration.failed(w, "list Avito ads", err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "state": state})
	}
}
func avitoImportHandler(administration adminHandlers, service avitoService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := administration.authorize(w, r, admin.PermissionProcurementEdit); !ok {
			return
		}
		count, err := service.Import(r.Context())
		if err != nil {
			administration.failed(w, "import Avito ads", err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"imported": count})
	}
}
func avitoProductsHandler(administration adminHandlers, service avitoService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := administration.authorize(w, r, admin.PermissionProcurementRead); !ok {
			return
		}
		items, err := service.SearchProducts(r.Context(), strings.TrimSpace(r.URL.Query().Get("q")))
		if err != nil {
			administration.failed(w, "search Avito products", err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}
func avitoMappingHandler(administration adminHandlers, service avitoService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := administration.authorize(w, r, admin.PermissionProcurementEdit); !ok {
			return
		}
		var input struct {
			ProductIDs []int64 `json:"productIds"`
		}
		if decodeJSON(r, &input) != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: "Некорректные товары"})
			return
		}
		if err := service.ReplaceProducts(r.Context(), r.PathValue("itemID"), input.ProductIDs); err != nil {
			administration.failed(w, "save Avito mapping", err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
	}
}
func avitoPublicationHandler(administration adminHandlers, service avitoService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := administration.authorize(w, r, admin.PermissionIntegrationsEdit); !ok {
			return
		}
		var input struct {
			Enabled bool `json:"enabled"`
		}
		if decodeJSON(r, &input) != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: "Некорректный режим"})
			return
		}
		if err := service.SetPublication(r.Context(), input.Enabled); err != nil {
			administration.failed(w, "set Avito publication", err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"enabled": input.Enabled})
	}
}
