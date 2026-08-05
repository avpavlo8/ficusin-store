package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/avpavlo8/ficusin-store/backend/internal/auth"
	"github.com/avpavlo8/ficusin-store/backend/internal/notify"
)

type pushService interface {
	PublicKey() string
	Subscribe(ctx context.Context, customerID *int64, subscription notify.Subscription) error
	Unsubscribe(ctx context.Context, endpoint string) error
}

// pushKeyHandler hands the browser the public half of the VAPID pair, which
// it needs before it can subscribe. An empty key is a normal answer: it tells
// the page that notifications are switched off.
func pushKeyHandler(push pushService) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		key := ""
		if push != nil {
			key = push.PublicKey()
		}
		writeJSON(response, http.StatusOK, map[string]any{
			"publicKey": key,
			"enabled":   key != "",
		})
	})
}

type subscriptionBody struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

func pushSubscribeHandler(
	logger *slog.Logger,
	authentication authService,
	push pushService,
) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if push == nil || push.PublicKey() == "" {
			writeJSON(response, http.StatusServiceUnavailable, errorResponse{
				Error: "Уведомления пока не настроены",
			})
			return
		}

		var body subscriptionBody
		if err := decodeJSON(request, &body); err != nil {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректная подписка"})
			return
		}
		endpoint := strings.TrimSpace(body.Endpoint)
		// The endpoint is a URL the push service gave the browser; anything
		// else is either a mistake or an attempt to make us call a stranger.
		if !strings.HasPrefix(endpoint, "https://") || len(endpoint) > 1000 {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректная подписка"})
			return
		}

		// Signing in is not required: a guest tracking an order is a fair
		// reason to want a notification.
		var customerID *int64
		if cookie, err := request.Cookie(auth.CookieName); err == nil {
			if user, lookupErr := authentication.UserByToken(request.Context(), cookie.Value); lookupErr == nil && user != nil {
				customerID = &user.ID
			}
		}

		if err := push.Subscribe(request.Context(), customerID, notify.Subscription{
			Endpoint:  endpoint,
			P256dh:    strings.TrimSpace(body.Keys.P256dh),
			Auth:      strings.TrimSpace(body.Keys.Auth),
			UserAgent: request.UserAgent(),
		}); err != nil {
			logger.Error("store push subscription failed", "error", err)
			writeJSON(response, http.StatusInternalServerError, errorResponse{
				Error: "Не удалось включить уведомления",
			})
			return
		}
		writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
	})
}

func pushUnsubscribeHandler(logger *slog.Logger, push pushService) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var body struct {
			Endpoint string `json:"endpoint"`
		}
		if err := decodeJSON(request, &body); err != nil {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректная подписка"})
			return
		}
		if push != nil {
			if err := push.Unsubscribe(request.Context(), strings.TrimSpace(body.Endpoint)); err != nil {
				logger.Error("delete push subscription failed", "error", err)
			}
		}
		writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
	})
}
