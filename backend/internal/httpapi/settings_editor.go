package httpapi

import (
	"net/http"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/settings"
)

// The product editor needs packaging season, not access to the global settings.
func editorSettingsHandler(h adminHandlers, config settingsService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := h.authorize(w, r, admin.PermissionProductsRead); !ok {
			return
		}
		if config == nil {
			writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "Настройки упаковки недоступны"})
			return
		}
		season := config.All()[settings.CDEKPackagingSeason]
		if season != "summer" && season != "winter" {
			writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "Не удалось определить сезон упаковки"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"values": map[string]string{settings.CDEKPackagingSeason: season},
		})
	}
}
