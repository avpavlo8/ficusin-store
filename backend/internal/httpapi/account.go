package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/avpavlo8/ficusin-store/backend/internal/auth"
	"github.com/avpavlo8/ficusin-store/backend/internal/order"
)

type orderRepository interface {
	ListForCustomer(context.Context, int64, int) ([]order.Summary, error)
}

func accountOrdersHandler(
	logger *slog.Logger,
	authentication authService,
	orders orderRepository,
) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		cookie, err := request.Cookie(auth.CookieName)
		if err != nil {
			writeJSON(response, http.StatusUnauthorized, errorResponse{Error: "Требуется авторизация"})
			return
		}

		user, err := authentication.UserByToken(request.Context(), cookie.Value)
		if err != nil {
			logger.Error("account session lookup failed", "error", err)
			writeJSON(response, http.StatusInternalServerError, errorResponse{
				Error: "Не удалось загрузить заказы",
			})
			return
		}
		if user == nil {
			writeJSON(response, http.StatusUnauthorized, errorResponse{Error: "Требуется авторизация"})
			return
		}

		items, err := orders.ListForCustomer(request.Context(), user.ID, 50)
		if err != nil {
			logger.Error("account orders failed", "error", err, "customer_id", user.ID)
			writeJSON(response, http.StatusServiceUnavailable, errorResponse{
				Error: "Не удалось загрузить заказы",
			})
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"orders": items})
	})
}
