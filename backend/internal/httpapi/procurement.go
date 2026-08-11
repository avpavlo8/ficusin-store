package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/procurement"
)

type procurementService interface {
	Dashboard(context.Context) (procurement.Dashboard, error)
	UpdateSettings(context.Context, procurement.Actor, procurement.PricingSettings) (procurement.PricingSettings, error)
	CreateSupplier(context.Context, procurement.Actor, procurement.SupplierCreate) (procurement.Supplier, error)
	CreateOrder(context.Context, procurement.Actor, procurement.OrderCreate) (procurement.OrderSummary, error)
	CreatePlan(context.Context, procurement.Actor, procurement.PlanCreate) (procurement.OrderSummary, error)
	OrderDetail(context.Context, int64) (procurement.OrderDetail, error)
	CalculateOrder(context.Context, procurement.Actor, int64, procurement.CalculationInput) (procurement.OrderDetail, error)
	ImportDocument(context.Context, procurement.Actor, procurement.DocumentUpload) (procurement.ImportResult, error)
	SearchNomenclature(context.Context, string) ([]procurement.NomenclatureCandidate, error)
	ResolveAlias(context.Context, procurement.Actor, int64, procurement.AliasResolution) (procurement.AliasReview, error)
	CreateRequest(context.Context, procurement.Actor, procurement.RequestCreate) (procurement.Request, error)
	UpdateAvailability(context.Context, procurement.Actor, int64, procurement.AvailabilityUpdate) (procurement.AliasReview, error)
	PrepareBatch(context.Context, procurement.Actor, int64, string) (procurement.ActionBatch, error)
	ApproveBatch(context.Context, procurement.Actor, int64) (procurement.ActionBatch, error)
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

func (handlers procurementHandlers) updateSettings(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.admin.authorize(response, request, admin.PermissionProcurementEdit)
	if !ok {
		return
	}
	var input procurement.PricingSettings
	if decodeJSON(request, &input) != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректные настройки расчёта"})
		return
	}
	item, err := handlers.service.UpdateSettings(request.Context(), procurement.Actor{CustomerID: actor.CustomerID, Role: actor.Role}, input)
	if err != nil {
		handlers.failed(response, "update procurement settings", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"settings": item})
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

func (handlers procurementHandlers) createPlan(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.admin.authorize(response, request, admin.PermissionProcurementEdit)
	if !ok {
		return
	}
	var input procurement.PlanCreate
	if decodeJSON(request, &input) != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректный список закупки"})
		return
	}
	item, err := handlers.service.CreatePlan(request.Context(), procurement.Actor{CustomerID: actor.CustomerID, Role: actor.Role}, input)
	if err != nil {
		handlers.failed(response, "create procurement plan", err)
		return
	}
	writeJSON(response, http.StatusCreated, map[string]any{"order": item})
}

func (handlers procurementHandlers) orderDetail(response http.ResponseWriter, request *http.Request) {
	if _, _, ok := handlers.admin.authorize(response, request, admin.PermissionProcurementRead); !ok {
		return
	}
	orderID, ok := pathID(response, request)
	if !ok {
		return
	}
	item, err := handlers.service.OrderDetail(request.Context(), orderID)
	if err != nil {
		handlers.failed(response, "load procurement order", err)
		return
	}
	writeJSON(response, http.StatusOK, item)
}

func (handlers procurementHandlers) calculateOrder(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.admin.authorize(response, request, admin.PermissionProcurementEdit)
	if !ok {
		return
	}
	orderID, ok := pathID(response, request)
	if !ok {
		return
	}
	var input procurement.CalculationInput
	if decodeJSON(request, &input) != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректные расходы закупки"})
		return
	}
	item, err := handlers.service.CalculateOrder(request.Context(), procurement.Actor{CustomerID: actor.CustomerID, Role: actor.Role}, orderID, input)
	if err != nil {
		handlers.failed(response, "calculate procurement order", err)
		return
	}
	writeJSON(response, http.StatusOK, item)
}

func (handlers procurementHandlers) importDocument(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.admin.authorize(response, request, admin.PermissionProcurementEdit)
	if !ok {
		return
	}
	if handlers.service == nil {
		writeJSON(response, http.StatusServiceUnavailable, errorResponse{Error: "Раздел закупок пока недоступен"})
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, (20<<20)+(1<<20))
	if err := request.ParseMultipartForm(20 << 20); err != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "PDF слишком большой или форма повреждена"})
		return
	}
	defer request.MultipartForm.RemoveAll() //nolint:errcheck
	supplierID, err := strconv.ParseInt(request.FormValue("supplierId"), 10, 64)
	if err != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Выберите поставщика"})
		return
	}
	var orderID int64
	if raw := request.FormValue("orderId"); raw != "" {
		orderID, err = strconv.ParseInt(raw, 10, 64)
		if err != nil {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректная закупка"})
			return
		}
	}
	file, header, err := request.FormFile("file")
	if err != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Выберите PDF-файл"})
		return
	}
	defer file.Close() //nolint:errcheck
	content, err := io.ReadAll(io.LimitReader(file, (20<<20)+1))
	if err != nil || len(content) > 20<<20 {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Не удалось прочитать PDF или файл больше 20 МБ"})
		return
	}
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = http.DetectContentType(content)
	}
	result, err := handlers.service.ImportDocument(request.Context(), procurement.Actor{
		CustomerID: actor.CustomerID, Role: actor.Role,
	}, procurement.DocumentUpload{
		SupplierID: supplierID, OrderID: orderID, FileName: header.Filename,
		ContentType: contentType, Content: content,
	})
	if err != nil {
		handlers.failed(response, "import procurement document", err)
		return
	}
	status := http.StatusCreated
	if result.Duplicate {
		status = http.StatusOK
	}
	writeJSON(response, status, result)
}

func (handlers procurementHandlers) searchNomenclature(response http.ResponseWriter, request *http.Request) {
	if _, _, ok := handlers.admin.authorize(response, request, admin.PermissionProcurementRead); !ok {
		return
	}
	if handlers.service == nil {
		writeJSON(response, http.StatusServiceUnavailable, errorResponse{Error: "Раздел закупок пока недоступен"})
		return
	}
	items, err := handlers.service.SearchNomenclature(request.Context(), request.URL.Query().Get("q"))
	if err != nil {
		handlers.failed(response, "search procurement nomenclature", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

func (handlers procurementHandlers) resolveAlias(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.admin.authorize(response, request, admin.PermissionProcurementEdit)
	if !ok {
		return
	}
	if handlers.service == nil {
		writeJSON(response, http.StatusServiceUnavailable, errorResponse{Error: "Раздел закупок пока недоступен"})
		return
	}
	aliasID, ok := pathID(response, request)
	if !ok {
		return
	}
	var input procurement.AliasResolution
	if decodeJSON(request, &input) != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректное решение по сопоставлению"})
		return
	}
	item, err := handlers.service.ResolveAlias(request.Context(), procurement.Actor{
		CustomerID: actor.CustomerID, Role: actor.Role,
	}, aliasID, input)
	if err != nil {
		handlers.failed(response, "resolve procurement alias", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"alias": item})
}

func (handlers procurementHandlers) createRequest(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.admin.authorize(response, request, admin.PermissionProcurementEdit)
	if !ok {
		return
	}
	var input procurement.RequestCreate
	if decodeJSON(request, &input) != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректный запрос на закупку"})
		return
	}
	item, err := handlers.service.CreateRequest(request.Context(), procurement.Actor{CustomerID: actor.CustomerID, Role: actor.Role}, input)
	if err != nil {
		handlers.failed(response, "create procurement request", err)
		return
	}
	writeJSON(response, http.StatusCreated, map[string]any{"request": item})
}

func (handlers procurementHandlers) updateAvailability(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.admin.authorize(response, request, admin.PermissionProcurementEdit)
	if !ok {
		return
	}
	aliasID, ok := pathID(response, request)
	if !ok {
		return
	}
	var input procurement.AvailabilityUpdate
	if decodeJSON(request, &input) != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректный статус наличия"})
		return
	}
	item, err := handlers.service.UpdateAvailability(request.Context(), procurement.Actor{CustomerID: actor.CustomerID, Role: actor.Role}, aliasID, input)
	if err != nil {
		handlers.failed(response, "update procurement availability", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"alias": item})
}

func (handlers procurementHandlers) prepareBatch(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.admin.authorize(response, request, admin.PermissionProcurementEdit)
	if !ok {
		return
	}
	orderID, ok := pathID(response, request)
	if !ok {
		return
	}
	var input struct {
		Kind string `json:"kind"`
	}
	if decodeJSON(request, &input) != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректный тип документа"})
		return
	}
	item, err := handlers.service.PrepareBatch(request.Context(), procurement.Actor{CustomerID: actor.CustomerID, Role: actor.Role}, orderID, input.Kind)
	if err != nil {
		handlers.failed(response, "prepare procurement batch", err)
		return
	}
	writeJSON(response, http.StatusCreated, map[string]any{"batch": item})
}

func (handlers procurementHandlers) approveBatch(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.admin.authorize(response, request, admin.PermissionProcurementEdit)
	if !ok {
		return
	}
	batchID, ok := pathID(response, request)
	if !ok {
		return
	}
	item, err := handlers.service.ApproveBatch(request.Context(), procurement.Actor{CustomerID: actor.CustomerID, Role: actor.Role}, batchID)
	if err != nil {
		handlers.failed(response, "approve procurement batch", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"batch": item})
}

func (handlers procurementHandlers) failed(response http.ResponseWriter, operation string, err error) {
	status := http.StatusServiceUnavailable
	message := "Не удалось выполнить операцию"
	if errors.Is(err, procurement.ErrInvalidInput) {
		status, message = http.StatusBadRequest, "Проверьте заполненные поля"
	}
	if errors.Is(err, procurement.ErrNotFound) {
		status, message = http.StatusNotFound, "Поставщик или закупка не найдены"
	}
	if errors.Is(err, procurement.ErrUnsupportedDocument) {
		status, message = http.StatusUnprocessableEntity, "Не удалось распознать строки PDF. Проверьте формат документа"
	}
	if errors.Is(err, procurement.ErrDuplicate) {
		status, message = http.StatusConflict, "Этот документ уже загружен"
	}
	handlers.logger.Error(operation+" failed", "error", err)
	writeJSON(response, status, errorResponse{Error: message})
}
