package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/jackc/pgx/v5"
)

type editorPriceRepository interface {
	EditorPrices(context.Context, int64) ([]admin.EditorPricePublication, error)
	EditorPricePublicationMode() string
	ApproveEditorPrice(context.Context, admin.Actor, int64, int64, string) (admin.EditorPricePublication, error)
	RetryEditorPrice(context.Context, admin.Actor, int64, int64, string) (admin.EditorPricePublication, error)
}

func editorPricesHandler(h adminHandlers) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := h.authorize(w, r, admin.PermissionProductsRead); !ok {
			return
		}
		productID, ok := pimPathID(w, r, "productId")
		if !ok {
			return
		}
		repo, ok := h.repository.(editorPriceRepository)
		if !ok {
			writeJSON(w, 503, errorResponse{Error: "Цены площадок недоступны"})
			return
		}
		prices, err := repo.EditorPrices(r.Context(), productID)
		if err != nil {
			editorPriceError(h, w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"prices": prices, "publicationMode": repo.EditorPricePublicationMode()})
	}
}
func editorPriceMutationHandler(h adminHandlers, retry bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, actor, ok := h.authorize(w, r, admin.PermissionProductsManage)
		if !ok {
			return
		}
		productID, ok := pimPathID(w, r, "productId")
		if !ok {
			return
		}
		priceID, ok := pimPathID(w, r, "priceId")
		if !ok {
			return
		}
		var input struct {
			Fingerprint string `json:"fingerprint"`
		}
		if decodeJSON(r, &input) != nil || strings.TrimSpace(input.Fingerprint) == "" {
			writeJSON(w, 400, errorResponse{Error: "Обновите предложение цены перед подтверждением"})
			return
		}
		repo, ok := h.repository.(editorPriceRepository)
		if !ok {
			writeJSON(w, 503, errorResponse{Error: "Цены площадок недоступны"})
			return
		}
		var result admin.EditorPricePublication
		var err error
		if retry {
			result, err = repo.RetryEditorPrice(r.Context(), actor, productID, priceID, input.Fingerprint)
		} else {
			result, err = repo.ApproveEditorPrice(r.Context(), actor, productID, priceID, input.Fingerprint)
		}
		if err != nil {
			editorPriceError(h, w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}
func editorPriceError(h adminHandlers, w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, pgx.ErrNoRows), errors.Is(err, admin.ErrPriceChannelUnavailable):
		writeJSON(w, 409, errorResponse{Error: "Предложение цены изменилось или канал недоступен. Обновите цены."})
	case errors.Is(err, admin.ErrForbidden):
		writeJSON(w, 403, errorResponse{Error: "Недостаточно прав"})
	case errors.Is(err, admin.ErrInvalidInput):
		writeJSON(w, 400, errorResponse{Error: err.Error()})
	default:
		h.failed(w, "editor price publication", err)
	}
}
