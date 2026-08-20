package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/avpavlo8/ficusin-store/backend/internal/auth"
	"github.com/avpavlo8/ficusin-store/backend/internal/payment"
)

type paymentService interface {
	Configured() bool
	Start(ctx context.Context, orderNumber string) (string, error)
	Sync(ctx context.Context, providerPaymentID string) error
}

type adjustablePaymentService interface {
	StartOutstanding(ctx context.Context, orderNumber string) (string, payment.Balance, error)
	SyncOutstanding(ctx context.Context, providerPaymentID string) error
}

// paymentMethodsHandler tells the checkout which options to draw. The list
// depends on who is asking and how they collect, so it is built here rather
// than hardcoded in the browser.
func paymentMethodsHandler(authentication authService, payments paymentService) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		delivery := strings.TrimSpace(request.URL.Query().Get("delivery"))
		wholesale := false
		if cookie, err := request.Cookie(auth.CookieName); err == nil {
			if user, err := authentication.UserByToken(request.Context(), cookie.Value); err == nil && user != nil {
				wholesale = user.WholesaleStatus == "approved"
			}
		}
		writeJSON(response, http.StatusOK, map[string]any{
			"methods": payment.Methods(delivery, wholesale, available(payments)),
		})
	})
}

// startPaymentHandler hands back the page to pay on. Mutable orders use the
// outstanding balance, not their original total. Most importantly, an order
// with a preorder or an unknown delivery price cannot open YooKassa at all.
func startPaymentHandler(logger *slog.Logger, payments paymentService) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if !available(payments) {
			writeJSON(response, http.StatusServiceUnavailable, errorResponse{
				Error: "Оплата картой временно недоступна",
			})
			return
		}
		orderNumber := strings.TrimSpace(request.PathValue("orderNumber"))
		if advanced, ok := payments.(adjustablePaymentService); ok {
			url, balance, err := advanced.StartOutstanding(request.Context(), orderNumber)
			if err != nil {
				logger.Info("payment not started", "error", err, "order", orderNumber)
				message := "Не удалось начать оплату. Проверьте заказ или попробуйте ещё раз позже"
				if !balance.Ready {
					message = "Заказ принят. Оплата будет доступна после подтверждения наличия и стоимости доставки менеджером"
				} else if balance.Due <= 0 {
					message = "Заказ уже полностью оплачен"
				}
				writeJSON(response, http.StatusConflict, errorResponse{Error: message})
				return
			}
			writeJSON(response, http.StatusOK, map[string]any{"confirmationUrl": url, "payment": balance})
			return
		}

		url, err := payments.Start(request.Context(), orderNumber)
		if err != nil {
			logger.Error("start payment failed", "error", err, "order", orderNumber)
			writeJSON(response, http.StatusBadRequest, errorResponse{
				Error: "Не удалось начать оплату. Проверьте заказ или попробуйте ещё раз позже",
			})
			return
		}
		writeJSON(response, http.StatusOK, map[string]string{"confirmationUrl": url})
	})
}

// yooKassaWebhookHandler reacts to a notification by asking YooKassa what
// actually happened. The notification itself is unsigned and reachable by
// anyone, so its body is treated as a nudge and nothing more.
func yooKassaWebhookHandler(logger *slog.Logger, payments paymentService) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var body struct {
			Event  string `json:"event"`
			Object struct {
				ID string `json:"id"`
			} `json:"object"`
		}
		if err := decodeJSON(request, &body); err != nil || body.Object.ID == "" {
			writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
			return
		}
		if available(payments) {
			var err error
			if advanced, ok := payments.(adjustablePaymentService); ok {
				err = advanced.SyncOutstanding(request.Context(), body.Object.ID)
			} else {
				err = payments.Sync(request.Context(), body.Object.ID)
			}
			if err != nil {
				logger.Error("payment sync failed", "error", err, "payment_id", body.Object.ID)
				writeJSON(response, http.StatusInternalServerError, errorResponse{Error: "retry"})
				return
			}
		}
		writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
	})
}

func available(payments paymentService) bool {
	return payments != nil && payments.Configured()
}
