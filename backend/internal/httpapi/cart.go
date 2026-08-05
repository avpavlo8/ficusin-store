package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/avpavlo8/ficusin-store/backend/internal/auth"
)

// cartStore keeps the cart of a signed-in customer between devices. The
// browser stays the source of truth while shopping; this is the copy that
// survives a new phone or a cleared browser.
type cartStore interface {
	Load(ctx context.Context, customerID int64) (map[string]int, error)
	Save(ctx context.Context, customerID int64, items map[string]int) error
}

// maximumCartLines caps what we are willing to store. A real cart is a
// handful of plants; anything larger is a script, not a shopper.
const maximumCartLines = 100

func cartHandler(logger *slog.Logger, authentication authService, store cartStore) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		cookie, err := request.Cookie(auth.CookieName)
		if err != nil {
			// A guest has no server-side cart, and that is not an error:
			// the browser keeps its own copy either way.
			writeJSON(response, http.StatusOK, map[string]any{"items": map[string]int{}})
			return
		}
		user, err := authentication.UserByToken(request.Context(), cookie.Value)
		if err != nil || user == nil {
			writeJSON(response, http.StatusOK, map[string]any{"items": map[string]int{}})
			return
		}

		if request.Method == http.MethodGet {
			items, err := store.Load(request.Context(), user.ID)
			if err != nil {
				logger.Error("load cart failed", "error", err, "customer_id", user.ID)
				writeJSON(response, http.StatusOK, map[string]any{"items": map[string]int{}})
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
		if err := store.Save(request.Context(), user.ID, items); err != nil {
			logger.Error("save cart failed", "error", err, "customer_id", user.ID)
			writeJSON(response, http.StatusInternalServerError, errorResponse{
				Error: "Не удалось сохранить корзину",
			})
			return
		}
		writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
	})
}
