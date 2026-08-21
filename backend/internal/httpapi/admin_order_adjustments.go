package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/payment"
)

type adminOrderAdjustmentRepository interface {
	EditOrder(context.Context, admin.Actor, int64, admin.OrderEdit) (admin.Order, error)
	OrderAdjustment(context.Context, int64) (admin.OrderAdjustmentState, error)
}

type adminOrderPaymentService interface {
	SupersedePending(context.Context, int64) error
	BalanceForOrder(context.Context, int64) (payment.Balance, error)
	Reconcile(context.Context, int64) (payment.Balance, error)
	RefundAmount(context.Context, int64, float64, string) (payment.Balance, error)
	RefundExcess(context.Context, int64, string) (payment.Balance, error)
	StartOutstandingForOrderID(context.Context, int64) (string, payment.Balance, error)
}

func (handlers adminHandlers) orderAdjustmentState(response http.ResponseWriter, request *http.Request) {
	_, _, ok := handlers.authorize(response, request, admin.PermissionOrdersRead)
	if !ok {
		return
	}
	id, ok := pathID(response, request)
	if !ok {
		return
	}
	repository, ok := handlers.repository.(adminOrderAdjustmentRepository)
	if !ok {
		handlers.failed(response, "order adjustment unavailable", errors.New("order adjustment unavailable"))
		return
	}
	state, err := repository.OrderAdjustment(request.Context(), id)
	if err != nil {
		handlers.failed(response, "load order adjustment", err)
		return
	}
	var balance payment.Balance
	if payments, able := handlers.payments.(adminOrderPaymentService); able {
		balance, err = payments.BalanceForOrder(request.Context(), id)
		if err != nil {
			handlers.failed(response, "load order payment balance", err)
			return
		}
	}
	writeJSON(response, http.StatusOK, map[string]any{"order": state, "payment": balance})
}

func (handlers adminHandlers) editOrderContents(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.authorize(response, request, admin.PermissionOrdersEdit)
	if !ok {
		return
	}
	id, ok := pathID(response, request)
	if !ok {
		return
	}
	repository, ok := handlers.repository.(adminOrderAdjustmentRepository)
	if !ok {
		handlers.failed(response, "order adjustment unavailable", errors.New("order adjustment unavailable"))
		return
	}
	var edit admin.OrderEdit
	if decodeJSON(request, &edit) != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректные данные заказа"})
		return
	}
	payments, hasPayments := handlers.payments.(adminOrderPaymentService)
	if hasPayments {
		// A payment page belongs to the amount that existed when it was
		// created. YooKassa cannot cancel a normal one-stage payment while it
		// is still pending confirmation, so provider cancellation must not be
		// a prerequisite for editing the order. Retire that attempt locally;
		// its provider id remains recorded and a late webhook can still
		// reconcile a payment made from the old page.
		if err := payments.SupersedePending(request.Context(), id); err != nil {
			handlers.failed(response, "supersede old order payment", err)
			return
		}
	}
	orderValue, err := repository.EditOrder(request.Context(), actor, id, edit)
	if err != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	var balance payment.Balance
	if hasPayments {
		balance, err = payments.Reconcile(request.Context(), id)
		if err != nil {
			handlers.failed(response, "reconcile edited order", err)
			return
		}
		// Refund only after the new order is fully known. If delivery or stock
		// still needs a manager, a premature refund could be wrong too.
		if balance.Ready && balance.Overpaid > 0 {
			balance, err = payments.RefundExcess(request.Context(), id, "Автовозврат после изменения состава или доставки заказа")
			if err != nil {
				handlers.logger.Error("automatic order excess refund failed", "error", err, "order_id", id)
				writeJSON(response, http.StatusBadGateway, errorResponse{Error: "Заказ изменён, но автоматический возврат не прошёл. Повторите возврат из карточки заказа"})
				return
			}
		}
	}
	state, _ := repository.OrderAdjustment(request.Context(), id)
	writeJSON(response, http.StatusOK, map[string]any{"order": orderValue, "adjustment": state, "payment": balance})
}

func (handlers adminHandlers) refundOrderAmount(response http.ResponseWriter, request *http.Request) {
	_, _, ok := handlers.authorize(response, request, admin.PermissionOrdersEdit)
	if !ok {
		return
	}
	id, ok := pathID(response, request)
	if !ok {
		return
	}
	payments, able := handlers.payments.(adminOrderPaymentService)
	if !able {
		writeJSON(response, http.StatusServiceUnavailable, errorResponse{Error: "Возврат недоступен: оплата не настроена"})
		return
	}
	var body struct {
		Amount float64 `json:"amount"`
		Reason string  `json:"reason"`
	}
	if decodeJSON(request, &body) != nil || body.Amount <= 0 {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Укажите сумму возврата"})
		return
	}
	balance, err := payments.RefundAmount(request.Context(), id, body.Amount, body.Reason)
	if err != nil {
		handlers.logger.Error("partial refund failed", "error", err, "order_id", id)
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"payment": balance})
}

func (handlers adminHandlers) createOrderPaymentLink(response http.ResponseWriter, request *http.Request) {
	_, _, ok := handlers.authorize(response, request, admin.PermissionOrdersEdit)
	if !ok {
		return
	}
	id, ok := pathID(response, request)
	if !ok {
		return
	}
	payments, able := handlers.payments.(adminOrderPaymentService)
	if !able {
		writeJSON(response, http.StatusServiceUnavailable, errorResponse{Error: "Оплата картой не настроена"})
		return
	}
	url, balance, err := payments.StartOutstandingForOrderID(request.Context(), id)
	if err != nil {
		writeJSON(response, http.StatusConflict, errorResponse{Error: err.Error()})
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"confirmationUrl": url, "payment": balance})
}
