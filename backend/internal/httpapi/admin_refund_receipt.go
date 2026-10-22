package httpapi

import (
	"context"
	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"net/http"
	"strconv"
)

func (handlers adminHandlers) confirmRefundReceipt(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := handlers.authorize(w, r, admin.PermissionPaymentsRefund)
	if !ok {
		return
	}
	orderID, ok := pathID(w, r)
	if !ok {
		return
	}
	refundID, err := strconv.ParseInt(r.PathValue("refundID"), 10, 64)
	if err != nil || refundID <= 0 {
		writeJSON(w, 400, errorResponse{Error: "Некорректный возврат"})
		return
	}
	var body struct {
		Confirmed bool `json:"confirmed"`
	}
	if decodeJSON(r, &body) != nil || !body.Confirmed {
		writeJSON(w, 400, errorResponse{Error: "Подтвердите, что чек возврата уже сформирован на кассе"})
		return
	}
	service, ok := handlers.payments.(interface {
		ConfirmRefundReceipt(context.Context, int64, int64, int64) error
	})
	if !ok {
		writeJSON(w, 503, errorResponse{Error: "Проверка чеков недоступна"})
		return
	}
	if err := service.ConfirmRefundReceipt(r.Context(), orderID, refundID, actor.CustomerID); err != nil {
		writeJSON(w, 409, errorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (handlers adminHandlers) resolveRefund(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := handlers.authorize(w, r, admin.PermissionPaymentsRefund)
	if !ok {
		return
	}
	orderID, ok := pathID(w, r)
	if !ok {
		return
	}
	refundID, err := strconv.ParseInt(r.PathValue("refundID"), 10, 64)
	if err != nil || refundID <= 0 {
		writeJSON(w, 400, errorResponse{Error: "Некорректный возврат"})
		return
	}
	var body struct {
		ProviderID string `json:"providerId"`
	}
	if decodeJSON(r, &body) != nil {
		writeJSON(w, 400, errorResponse{Error: "Укажите ID возврата из ЮKassa"})
		return
	}
	service, ok := handlers.payments.(interface {
		ResolveRefund(context.Context, int64, int64, string, int64) error
	})
	if !ok {
		writeJSON(w, 503, errorResponse{Error: "Сверка возвратов недоступна"})
		return
	}
	if err := service.ResolveRefund(r.Context(), orderID, refundID, body.ProviderID, actor.CustomerID); err != nil {
		writeJSON(w, 409, errorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
