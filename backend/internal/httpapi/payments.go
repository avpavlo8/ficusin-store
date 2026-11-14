package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/auth"
	"github.com/avpavlo8/ficusin-store/backend/internal/payment"
)

type paymentService interface {
	Configured() bool
	AuthorizeOrderPayment(context.Context, string, string, int64) (bool, error)
	StartOutstanding(ctx context.Context, orderNumber string) (string, payment.Balance, error)
	SyncOutstanding(ctx context.Context, providerPaymentID string) error
	StartShipmentOffer(context.Context, string, int64) (string, error)
}

func startShipmentOfferPaymentHandler(logger *slog.Logger, authentication authService, payments paymentService) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if authentication == nil {
			writeJSON(response, http.StatusUnauthorized, errorResponse{Error: "Требуется авторизация"})
			return
		}
		cookie, err := request.Cookie(auth.CookieName)
		if err != nil {
			writeJSON(response, http.StatusUnauthorized, errorResponse{Error: "Требуется авторизация"})
			return
		}
		user, err := authentication.UserByToken(request.Context(), cookie.Value)
		if err != nil || user == nil {
			writeJSON(response, http.StatusUnauthorized, errorResponse{Error: "Требуется авторизация"})
			return
		}
		if !available(payments) {
			writeJSON(response, http.StatusServiceUnavailable, errorResponse{Error: "Оплата картой временно недоступна"})
			return
		}
		url, err := payments.StartShipmentOffer(request.Context(), request.PathValue("token"), user.ID)
		if err != nil {
			logger.Info("shipment offer payment not started", "error", err)
			writeJSON(response, http.StatusConflict, errorResponse{Error: err.Error()})
			return
		}
		writeJSON(response, http.StatusOK, map[string]string{"confirmationUrl": url})
	})
}

// paymentMethodsHandler tells the checkout which options to draw. The list
// depends on who is asking and how they collect, so it is built here rather
// than hardcoded in the browser.
func paymentMethodsHandler(authentication authService, payments paymentService) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		delivery := strings.TrimSpace(request.URL.Query().Get("delivery"))
		wholesale := false
		if authentication != nil {
			if cookie, err := request.Cookie(auth.CookieName); err == nil {
				if user, err := authentication.UserByToken(request.Context(), cookie.Value); err == nil && user != nil {
					wholesale = user.WholesaleStatus == "approved"
				}
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
func startPaymentHandler(logger *slog.Logger, authentication authService, payments paymentService) http.Handler {
	perOrder := newRateLimiter(10, time.Minute)
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if !available(payments) {
			writeJSON(response, http.StatusServiceUnavailable, errorResponse{
				Error: "Оплата картой временно недоступна",
			})
			return
		}
		orderNumber := strings.TrimSpace(request.PathValue("orderNumber"))
		var customerID int64
		if authentication != nil {
			if cookie, err := request.Cookie(auth.CookieName); err == nil {
				if user, err := authentication.UserByToken(request.Context(), cookie.Value); err == nil && user != nil {
					customerID = user.ID
				}
			}
		}
		allowed, err := payments.AuthorizeOrderPayment(request.Context(), orderNumber, request.Header.Get("X-Order-Payment-Token"), customerID)
		if err != nil {
			logger.Error("authorize order payment failed", "error", err)
		}
		if !allowed {
			writeJSON(response, http.StatusNotFound, errorResponse{Error: "Заказ недоступен. Войдите по телефону, указанному при оформлении"})
			return
		}
		// Unauthenticated guesses cannot exhaust a real customer's order limit.
		if !perOrder.allow(orderNumber) {
			writeJSON(response, http.StatusTooManyRequests, errorResponse{Error: "Слишком много попыток оплаты. Попробуйте позже"})
			return
		}
		url, balance, err := payments.StartOutstanding(request.Context(), orderNumber)
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
		balance.Issues = nil
		balance.Refunds = nil
		writeJSON(response, http.StatusOK, map[string]any{"confirmationUrl": url, "payment": balance})
	})
}

// yooKassaWebhookHandler reacts to a notification by asking YooKassa what
// actually happened. The notification itself is unsigned and reachable by
// anyone, so its body is treated as a nudge and nothing more.
func yooKassaWebhookHandler(logger *slog.Logger, payments paymentService) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var body struct {
			Type   string                     `json:"type"`
			Event  string                     `json:"event"`
			Object map[string]json.RawMessage `json:"object"`
		}
		// The provider sends a complete object. Keep strict, bounded envelope
		// parsing, but extract only its ID; status and money are verified by GET.
		var id string
		if err := decodeJSON(request, &body); err != nil ||
			body.Type != "notification" || body.Event == "" ||
			json.Unmarshal(body.Object["id"], &id) != nil || strings.TrimSpace(id) == "" {
			writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
			return
		}
		if !strings.HasPrefix(body.Event, "payment.") {
			writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
			return
		}
		if available(payments) {
			err := payments.SyncOutstanding(request.Context(), strings.TrimSpace(id))
			if err != nil {
				reference := sha256.Sum256([]byte(strings.TrimSpace(id)))
				logger.Error("payment sync failed", "error_type", fmt.Sprintf("%T", err), "payment_ref", fmt.Sprintf("%x", reference))
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
