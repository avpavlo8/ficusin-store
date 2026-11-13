package httpapi

import (
	"context"
	"errors"
	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/ordersale"
	"net/http"
	"strconv"
	"strings"
)

type orderSaleRepository interface {
	OrderRealizations(context.Context, admin.Actor, string, int) (ordersale.View, error)
	PrepareOrderRealizations(context.Context, admin.Actor, ordersale.PrepareInput) ([]ordersale.Event, error)
}

func registerOrderSaleRoutes(mux *http.ServeMux, h adminHandlers) {
	mux.HandleFunc("GET /api/v1/admin/order-realizations", orderSaleHandler(h, false))
	mux.HandleFunc("POST /api/v1/admin/order-realizations/prepare", orderSaleHandler(h, true))
}
func orderSaleHandler(h adminHandlers, prepare bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, actor, ok := h.authorize(w, r, admin.PermissionOrdersRead)
		if !ok {
			return
		}
		if actor.Role != admin.RoleOwner {
			writeJSON(w, 403, errorResponse{Error: "Реализации доступны владельцу"})
			return
		}
		repo, ok := h.repository.(orderSaleRepository)
		if !ok {
			writeJSON(w, 503, errorResponse{Error: "Подготовка реализаций недоступна"})
			return
		}
		if !prepare {
			search := strings.TrimSpace(r.URL.Query().Get("q"))
			offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
			if r.URL.Query().Get("offset") == "" {
				offset = 0
				err = nil
			}
			if err != nil || offset < 0 || offset > 100000 || len(search) > 100 {
				writeJSON(w, 400, errorResponse{Error: "Проверьте параметры поиска"})
				return
			}
			view, err := repo.OrderRealizations(r.Context(), actor, search, offset)
			if err != nil {
				h.failed(w, "list order realizations", err)
				return
			}
			writeJSON(w, 200, view)
			return
		}
		var input ordersale.PrepareInput
		if decodeJSON(r, &input) != nil || input.WarehouseID <= 0 || len(input.Events) == 0 || len(input.Events) > 50 {
			writeJSON(w, 400, errorResponse{Error: "Выберите склад и от 1 до 50 отправок"})
			return
		}
		for _, s := range input.Events {
			if s.OrderID <= 0 || s.ShipmentOfferID < 0 || len(s.Fingerprint) != 64 {
				writeJSON(w, 400, errorResponse{Error: "Обновите список перед подготовкой"})
				return
			}
		}
		events, err := repo.PrepareOrderRealizations(r.Context(), actor, input)
		if errors.Is(err, ordersale.ErrInput) {
			writeJSON(w, 400, errorResponse{Error: err.Error()})
			return
		}
		if errors.Is(err, ordersale.ErrConflict) || errors.Is(err, ordersale.ErrNotReady) {
			writeJSON(w, 409, errorResponse{Error: err.Error()})
			return
		}
		if err != nil {
			h.failed(w, "prepare order realization", err)
			return
		}
		writeJSON(w, 200, map[string]any{"events": events, "externalWritesEnabled": false})
	}
}
