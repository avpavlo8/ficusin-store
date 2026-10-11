package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
)

type channelScopeRepository interface {
	SetVariantMarketplaceScope(context.Context, admin.Actor, int64, string) error
}

func variantMarketplaceScopeHandler(handlers adminHandlers) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		_, actor, ok := handlers.authorize(response, request, admin.PermissionProductsManage)
		if !ok {
			return
		}
		if actor.Role != admin.RoleOwner {
			writeJSON(response, http.StatusForbidden, errorResponse{Error: "Каналы продаж меняет владелец"})
			return
		}
		variantID, ok := pimPathID(response, request, "variantId")
		if !ok {
			return
		}
		var input struct {
			MarketplaceScope string `json:"marketplaceScope"`
		}
		if decodeJSON(request, &input) != nil {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Укажите каналы продаж"})
			return
		}
		if input.MarketplaceScope != "unknown" && input.MarketplaceScope != "site_only" && input.MarketplaceScope != "marketplaces" {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Неизвестное правило каналов продаж"})
			return
		}
		repository, ok := handlers.repository.(channelScopeRepository)
		if !ok {
			handlers.failed(response, "channel scope unavailable", errors.New("channel scope unavailable"))
			return
		}
		if err := repository.SetVariantMarketplaceScope(request.Context(), actor, variantID, input.MarketplaceScope); err != nil {
			if errors.Is(err, admin.ErrInvalidInput) {
				writeJSON(response, http.StatusBadRequest, errorResponse{Error: err.Error()})
				return
			}
			handlers.failed(response, "update channel scope", err)
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"marketplaceScope": input.MarketplaceScope})
	}
}
