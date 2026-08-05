package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/auth"
	"github.com/avpavlo8/ficusin-store/backend/internal/settings"
)

type settingsService interface {
	All() map[string]string
	Save(ctx context.Context, changes map[string]string) error
}

// settingsHandlers expose the switches the owner may flip. Only the owner:
// turning payments off or the sender address wrong is not a manager's call.
type settingsHandlers struct {
	logger   *slog.Logger
	auth     authService
	settings settingsService
}

func (handlers settingsHandlers) get(response http.ResponseWriter, request *http.Request) {
	if !handlers.allowed(response, request) {
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"definitions": settings.Definitions,
		"values":      handlers.settings.All(),
	})
}

func (handlers settingsHandlers) update(response http.ResponseWriter, request *http.Request) {
	if !handlers.allowed(response, request) {
		return
	}
	var body struct {
		Values map[string]string `json:"values"`
	}
	if err := decodeJSON(request, &body); err != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректные настройки"})
		return
	}
	if err := handlers.settings.Save(request.Context(), body.Values); err != nil {
		handlers.logger.Error("save settings failed", "error", err)
		writeJSON(response, http.StatusInternalServerError, errorResponse{
			Error: "Не удалось сохранить настройки",
		})
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"definitions": settings.Definitions,
		"values":      handlers.settings.All(),
	})
}

func (handlers settingsHandlers) allowed(response http.ResponseWriter, request *http.Request) bool {
	if handlers.settings == nil {
		writeJSON(response, http.StatusServiceUnavailable, errorResponse{
			Error: "Настройки недоступны",
		})
		return false
	}
	cookie, err := request.Cookie(auth.CookieName)
	if err != nil {
		writeJSON(response, http.StatusUnauthorized, errorResponse{Error: "Требуется авторизация"})
		return false
	}
	// The role comes from admin_users, like everywhere else in the panel.
	user, err := handlers.auth.UserByToken(request.Context(), cookie.Value)
	if err != nil || user == nil {
		writeJSON(response, http.StatusUnauthorized, errorResponse{Error: "Требуется авторизация"})
		return false
	}
	if user.AdminRole != admin.RoleOwner {
		writeJSON(response, http.StatusForbidden, errorResponse{
			Error: "Настройки доступны только владельцу",
		})
		return false
	}
	return true
}
