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

// startPaymentHandler hands back the page to pay on. It is deliberately
// callable for an order the customer already has: coming back to an unpaid
// order and paying it later is normal.
func startPaymentHandler(logger *slog.Logger, payments paymentService) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if !available(payments) {
			writeJSON(response, http.StatusServiceUnavailable, errorResponse{
				Error: "Оплата картой временно недоступна",
			})
			return
		}
		orderNumber := strings.TrimSpace(request.PathValue("orderNumber"))
		url, err := payments.Start(request.Context(), orderNumber)
		if err != nil {
			logger.Error("start payment failed", "error", err, "order", orderNumber)
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: err.Error()})
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
			// Answering 200 stops YooKassa from retrying something we will
			// never understand.
			writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
			return
		}
		if available(payments) {
			if err := payments.Sync(request.Context(), body.Object.ID); err != nil {
				logger.Error("payment sync failed", "error", err, "payment_id", body.Object.ID)
				// A 500 makes YooKassa try again later, which is what we
				// want when our own database is the thing that failed.
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
