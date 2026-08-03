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
	DetailForCustomer(ctx context.Context, customerID int64, orderNumber string) (*order.Detail, error)
}

// accountOrderHandler serves one order for the account page. The lookup is
// scoped to the signed-in customer, so an order number from somebody else
// simply reads as "not found".
func accountOrderHandler(
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
				Error: "Не удалось загрузить заказ",
			})
			return
		}
		if user == nil {
			writeJSON(response, http.StatusUnauthorized, errorResponse{Error: "Требуется авторизация"})
			return
		}

		detail, err := orders.DetailForCustomer(
			request.Context(),
			user.ID,
			request.PathValue("orderNumber"),
		)
		if err != nil {
			logger.Error("account order failed", "error", err, "customer_id", user.ID)
			writeJSON(response, http.StatusServiceUnavailable, errorResponse{
				Error: "Не удалось загрузить заказ",
			})
			return
		}
		if detail == nil {
			writeJSON(response, http.StatusNotFound, errorResponse{Error: "Заказ не найден"})
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"order": detail})
	})
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
