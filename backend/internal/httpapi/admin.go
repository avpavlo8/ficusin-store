package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/auth"
)

type adminRepository interface {
	Dashboard(context.Context) (admin.Dashboard, error)
}

func adminDashboardHandler(
	logger *slog.Logger,
	authentication authService,
	repository adminRepository,
	adminEmails map[string]struct{},
) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		cookie, err := request.Cookie(auth.CookieName)
		if err != nil {
			writeJSON(response, http.StatusUnauthorized, errorResponse{Error: "Требуется авторизация"})
			return
		}
		user, err := authentication.UserByToken(request.Context(), cookie.Value)
		if err != nil {
			logger.Error("admin session lookup failed", "error", err)
			writeJSON(response, http.StatusInternalServerError, errorResponse{Error: "Не удалось открыть панель"})
			return
		}
		if user == nil {
			writeJSON(response, http.StatusUnauthorized, errorResponse{Error: "Требуется авторизация"})
			return
		}
		if _, allowed := adminEmails[strings.ToLower(user.Email)]; !allowed {
			writeJSON(response, http.StatusForbidden, errorResponse{Error: "Недостаточно прав"})
			return
		}
		dashboard, err := repository.Dashboard(request.Context())
		if err != nil {
			logger.Error("admin dashboard failed", "error", err)
			writeJSON(response, http.StatusServiceUnavailable, errorResponse{Error: "Не удалось загрузить данные"})
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"user": user, "dashboard": dashboard})
	})
}
