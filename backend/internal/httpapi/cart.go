package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/auth"
)

type cartStore interface {
	Load(ctx context.Context, customerID int64) (map[string]int, error)
	Save(ctx context.Context, customerID int64, items map[string]int) error
	LoadGuest(ctx context.Context, tokenHash string) (map[string]int, error)
	SaveGuest(ctx context.Context, tokenHash string, items map[string]int, expiresAt time.Time) error
}

// maximumCartLines caps what we are willing to store. A real cart is a
// handful of plants; anything larger is a script, not a shopper.
const maximumCartLines = 100
const cartCookieName = "ficusin_cart_session"
const cartCookieLifetime = 365 * 24 * time.Hour

func newCartToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func hashCartToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func cartHandler(logger *slog.Logger, authentication authService, store cartStore, cookieSecure bool) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var customerID int64
		if cookie, err := request.Cookie(auth.CookieName); err == nil {
			if user, err := authentication.UserByToken(request.Context(), cookie.Value); err == nil && user != nil {
				customerID = user.ID
			}
		}
		guestToken := ""
		if customerID == 0 {
			if cookie, err := request.Cookie(cartCookieName); err == nil {
				guestToken = strings.TrimSpace(cookie.Value)
			}
			if guestToken == "" {
				var err error
				guestToken, err = newCartToken()
				if err != nil {
					logger.Error("create cart token failed", "error", err)
					writeJSON(response, http.StatusInternalServerError, errorResponse{Error: "Не удалось открыть корзину"})
					return
				}
			}
			http.SetCookie(response, &http.Cookie{
				Name: cartCookieName, Value: guestToken, Path: "/",
				MaxAge: int(cartCookieLifetime.Seconds()), Expires: time.Now().Add(cartCookieLifetime),
				HttpOnly: true, Secure: cookieSecure, SameSite: http.SameSiteLaxMode,
			})
		}

		if request.Method == http.MethodGet {
			var items map[string]int
			var err error
			if customerID != 0 {
				items, err = store.Load(request.Context(), customerID)
			} else {
				items, err = store.LoadGuest(request.Context(), hashCartToken(guestToken))
			}
			if err != nil {
				logger.Error("load cart failed", "error", err, "customer_id", customerID)
				writeJSON(response, http.StatusInternalServerError, errorResponse{Error: "Не удалось загрузить корзину"})
				return
			}
			writeJSON(response, http.StatusOK, map[string]any{"items": items})
			return
		}

		var body struct {
			Items map[string]int `json:"items"`
		}
		if err := decodeJSON(request, &body); err != nil {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректная корзина"})
			return
		}
		items := make(map[string]int, len(body.Items))
		for id, quantity := range body.Items {
			id = strings.TrimSpace(id)
			if id == "" || len(id) > 100 || len(items) >= maximumCartLines {
				continue
			}
			if quantity > 0 {
				items[id] = min(20, quantity)
			}
		}
		var saveErr error
		if customerID != 0 {
			saveErr = store.Save(request.Context(), customerID, items)
		} else {
			saveErr = store.SaveGuest(request.Context(), hashCartToken(guestToken), items, time.Now().Add(cartCookieLifetime))
		}
		if saveErr != nil {
			logger.Error("save cart failed", "error", saveErr, "customer_id", customerID)
			writeJSON(response, http.StatusInternalServerError, errorResponse{
				Error: "Не удалось сохранить корзину",
			})
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"items": items})
	})
}
