package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/procurement"
)

type procurementService interface {
	Dashboard(context.Context) (procurement.Dashboard, error)
	CreateSupplier(context.Context, procurement.Actor, procurement.SupplierCreate) (procurement.Supplier, error)
	CreateOrder(context.Context, procurement.Actor, procurement.OrderCreate) (procurement.OrderSummary, error)
}

type procurementHandlers struct {
	logger  *slog.Logger
	admin   adminHandlers
	service procurementService
}

func newProcurementHandlers(logger *slog.Logger, administration adminHandlers, service procurementService) procurementHandlers {
	return procurementHandlers{logger: logger, admin: administration, service: service}
}

func (handlers procurementHandlers) dashboard(response http.ResponseWriter, request *http.Request) {
	if _, _, ok := handlers.admin.authorize(response, request, admin.PermissionProcurementRead); !ok {
		return
	}
	if handlers.service == nil {
		writeJSON(response, http.StatusServiceUnavailable, errorResponse{Error: "Раздел закупок пока недоступен"})
		return
	}
	result, err := handlers.service.Dashboard(request.Context())
	if err != nil {
		handlers.failed(response, "procurement dashboard", err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (handlers procurementHandlers) createSupplier(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.admin.authorize(response, request, admin.PermissionProcurementEdit)
	if !ok {
		return
	}
	if handlers.service == nil {
		writeJSON(response, http.StatusServiceUnavailable, errorResponse{Error: "Раздел закупок пока недоступен"})
		return
	}
	var input procurement.SupplierCreate
	if decodeJSON(request, &input) != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректные данные поставщика"})
		return
	}
	item, err := handlers.service.CreateSupplier(request.Context(), procurement.Actor{
		CustomerID: actor.CustomerID, Role: actor.Role,
	}, input)
	if err != nil {
		handlers.failed(response, "create procurement supplier", err)
		return
	}
	writeJSON(response, http.StatusCreated, map[string]any{"supplier": item})
}

func (handlers procurementHandlers) createOrder(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.admin.authorize(response, request, admin.PermissionProcurementEdit)
	if !ok {
		return
	}
	if handlers.service == nil {
		writeJSON(response, http.StatusServiceUnavailable, errorResponse{Error: "Раздел закупок пока недоступен"})
		return
	}
	var input procurement.OrderCreate
	if decodeJSON(request, &input) != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректные данные закупки"})
		return
	}
	item, err := handlers.service.CreateOrder(request.Context(), procurement.Actor{
		CustomerID: actor.CustomerID, Role: actor.Role,
	}, input)
	if err != nil {
		handlers.failed(response, "create procurement order", err)
		return
	}
	writeJSON(response, http.StatusCreated, map[string]any{"order": item})
}

func (handlers procurementHandlers) failed(response http.ResponseWriter, operation string, err error) {
	status := http.StatusServiceUnavailable
	message := "Не удалось выполнить операцию"
	if errors.Is(err, procurement.ErrInvalidInput) {
		status, message = http.StatusBadRequest, "Проверьте заполненные поля"
	}
	if errors.Is(err, procurement.ErrNotFound) {
		status, message = http.StatusNotFound, "Поставщик не найден или отключён"
	}
	handlers.logger.Error(operation+" failed", "error", err)
	writeJSON(response, status, errorResponse{Error: message})
}
