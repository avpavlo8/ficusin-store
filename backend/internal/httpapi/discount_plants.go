package httpapi

import (
	"context"
	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"net/http"
)

type discountPlantRepository interface {
	DiscountPlants(context.Context) ([]admin.DiscountPlant, error)
	DiscountPlantPhoto(context.Context, int64) (string, []byte, error)
}

func (h adminHandlers) discountPlants(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.repository.(discountPlantRepository)
	if !ok {
		writeJSON(w, 503, errorResponse{Error: "Раздел временно недоступен"})
		return
	}
	items, err := repo.DiscountPlants(r.Context())
	if err != nil {
		h.failed(w, "discount plants", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"items": items})
}
func (h adminHandlers) discountPlantPhoto(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.repository.(discountPlantRepository)
	if !ok {
		http.NotFound(w, r)
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	mime, data, err := repo.DiscountPlantPhoto(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(data)
}
func (h adminHandlers) marketplaceReturn(w http.ResponseWriter, r *http.Request) {
	_, _, ok := h.authorize(w, r, admin.PermissionReturnsRead)
	if !ok {
		return
	}
	repo, ok := h.repository.(interface {
		MarketplaceReturn(context.Context, int64) (admin.MarketplaceReturn, error)
	})
	if !ok {
		http.NotFound(w, r)
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	item, err := repo.MarketplaceReturn(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, 200, map[string]any{"item": item})
}
