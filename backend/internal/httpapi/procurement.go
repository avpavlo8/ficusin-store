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
	DeleteSupplier(context.Context, procurement.Actor, int64) error
	CreateOrder(context.Context, procurement.Actor, procurement.OrderCreate) (procurement.OrderSummary, error)
	CreatePlan(context.Context, procurement.Actor, procurement.PlanCreate) (procurement.OrderSummary, error)
	OrderDetail(context.Context, int64) (procurement.OrderDetail, error)
	SabyPriceXLSX(context.Context, int64) ([]byte, string, error)
	CalculateOrder(context.Context, procurement.Actor, int64, procurement.CalculationInput) (procurement.OrderDetail, error)
	UpdateOrderStatus(context.Context, procurement.Actor, int64, procurement.OrderStatusUpdate) (procurement.OrderDetail, error)
	DeleteOrder(context.Context, procurement.Actor, int64) error
	UpdateOrderLine(context.Context, procurement.Actor, int64, procurement.OrderLineUpdate) (procurement.OrderDetail, error)
	ImportDocument(context.Context, procurement.Actor, procurement.DocumentUpload) (procurement.ImportResult, error)
	SearchNomenclature(context.Context, string) ([]procurement.NomenclatureCandidate, error)
	ResolveAlias(context.Context, procurement.Actor, int64, procurement.AliasResolution) (procurement.AliasReview, error)
	CreateRequest(context.Context, procurement.Actor, procurement.RequestCreate) (procurement.Request, error)
	UpdateRequest(context.Context, procurement.Actor, int64, procurement.RequestUpdate) (procurement.Request, error)
	UpdateAvailability(context.Context, procurement.Actor, procurement.AvailabilityUpdate) (procurement.AvailabilityItem, error)
	SetExclusion(context.Context, procurement.Actor, procurement.ExclusionUpdate) error
	ListProducts(context.Context, int64, string) ([]procurement.ProductDirectoryItem, error)
	UpdateProduct(context.Context, procurement.Actor, procurement.ProductDirectoryUpdate) (procurement.ProductDirectoryItem, error)
	PrepareBatch(context.Context, procurement.Actor, int64, string, []string) (procurement.ActionBatch, error)
	ApproveBatch(context.Context, procurement.Actor, int64) (procurement.ActionBatch, error)
	RetryBatch(context.Context, procurement.Actor, int64) (procurement.ActionBatch, error)
	CheckIntegration(context.Context, procurement.Actor, string) (procurement.IntegrationHealth, error)
	SyncChannelCatalog(context.Context, procurement.Actor, string) (procurement.ChannelLinkResult, error)
}

func (handlers procurementHandlers) checkIntegration(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.admin.authorize(response, request, admin.PermissionProcurementEdit)
	if !ok {
		return
	}
	channel := request.PathValue("channel")
	item, err := handlers.service.CheckIntegration(request.Context(), procurement.Actor{
		CustomerID: actor.CustomerID, Role: actor.Role,
	}, channel)
	if err != nil {
		handlers.failed(response, "check procurement integration", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"integration": item})
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

func (handlers procurementHandlers) deleteSupplier(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.admin.authorize(response, request, admin.PermissionProcurementEdit)
	if !ok {
		return
	}
	supplierID, ok := pathID(response, request)
	if !ok {
		return
	}
	if err := handlers.service.DeleteSupplier(request.Context(), procurement.Actor{
		CustomerID: actor.CustomerID, Role: actor.Role,
	}, supplierID); err != nil {
		handlers.failed(response, "delete procurement supplier", err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
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

func (handlers procurementHandlers) sabyPriceXLSX(response http.ResponseWriter, request *http.Request) {
	if _, _, ok := handlers.admin.authorize(response, request, admin.PermissionProcurementRead); !ok {
		return
	}
	orderID, ok := pathID(response, request)
	if !ok {
		return
	}
	content, name, err := handlers.service.SabyPriceXLSX(request.Context(), orderID)
	if err != nil {
		handlers.failed(response, "build Saby price XLSX", err)
		return
	}
	response.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	response.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write(content)
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

func (handlers procurementHandlers) updateOrderStatus(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.admin.authorize(response, request, admin.PermissionProcurementEdit)
	if !ok {
		return
	}
	orderID, ok := pathID(response, request)
	if !ok {
		return
	}
	var input procurement.OrderStatusUpdate
	if decodeJSON(request, &input) != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректный статус закупки"})
		return
	}
	item, err := handlers.service.UpdateOrderStatus(request.Context(), procurement.Actor{CustomerID: actor.CustomerID, Role: actor.Role}, orderID, input)
	if err != nil {
		handlers.failed(response, "update procurement order status", err)
		return
	}
	writeJSON(response, http.StatusOK, item)
}

func (handlers procurementHandlers) deleteOrder(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.admin.authorize(response, request, admin.PermissionProcurementEdit)
	if !ok {
		return
	}
	orderID, ok := pathID(response, request)
	if !ok {
		return
	}
	if err := handlers.service.DeleteOrder(request.Context(), procurement.Actor{CustomerID: actor.CustomerID, Role: actor.Role}, orderID); err != nil {
		handlers.failed(response, "delete procurement order", err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (handlers procurementHandlers) updateOrderLine(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.admin.authorize(response, request, admin.PermissionProcurementEdit)
	if !ok {
		return
	}
	lineID, ok := pathID(response, request)
	if !ok {
		return
	}
	var input procurement.OrderLineUpdate
	if decodeJSON(request, &input) != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректные данные строки"})
		return
	}
	item, err := handlers.service.UpdateOrderLine(request.Context(), procurement.Actor{CustomerID: actor.CustomerID, Role: actor.Role}, lineID, input)
	if err != nil {
		handlers.failed(response, "update procurement order line", err)
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

func (handlers procurementHandlers) updateRequest(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.admin.authorize(response, request, admin.PermissionProcurementEdit)
	if !ok {
		return
	}
	requestID, ok := pathID(response, request)
	if !ok {
		return
	}
	var input procurement.RequestUpdate
	if decodeJSON(request, &input) != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректные данные заявки"})
		return
	}
	item, err := handlers.service.UpdateRequest(request.Context(), procurement.Actor{CustomerID: actor.CustomerID, Role: actor.Role}, requestID, input)
	if err != nil {
		handlers.failed(response, "update procurement request", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"request": item})
}

func (handlers procurementHandlers) listProducts(response http.ResponseWriter, request *http.Request) {
	if _, _, ok := handlers.admin.authorize(response, request, admin.PermissionProcurementRead); !ok {
		return
	}
	var supplierID int64
	var err error
	if raw := request.URL.Query().Get("supplierId"); raw != "" {
		supplierID, err = strconv.ParseInt(raw, 10, 64)
		if err != nil {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректный поставщик"})
			return
		}
	}
	items, err := handlers.service.ListProducts(request.Context(), supplierID, request.URL.Query().Get("q"))
	if err != nil {
		handlers.failed(response, "list procurement products", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

func (handlers procurementHandlers) updateProduct(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.admin.authorize(response, request, admin.PermissionProcurementEdit)
	if !ok {
		return
	}
	var input procurement.ProductDirectoryUpdate
	if decodeJSON(request, &input) != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректная карточка закупки"})
		return
	}
	item, err := handlers.service.UpdateProduct(request.Context(), procurement.Actor{CustomerID: actor.CustomerID, Role: actor.Role}, input)
	if err != nil {
		handlers.failed(response, "update procurement product", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"item": item})
}

func (handlers procurementHandlers) updateAvailability(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.admin.authorize(response, request, admin.PermissionProcurementEdit)
	if !ok {
		return
	}
	var input procurement.AvailabilityUpdate
	if decodeJSON(request, &input) != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректный статус наличия"})
		return
	}
	item, err := handlers.service.UpdateAvailability(request.Context(), procurement.Actor{CustomerID: actor.CustomerID, Role: actor.Role}, input)
	if err != nil {
		handlers.failed(response, "update procurement availability", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"availability": item})
}

func (handlers procurementHandlers) syncChannelCatalog(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.admin.authorize(response, request, admin.PermissionProcurementEdit)
	if !ok {
		return
	}
	result, err := handlers.service.SyncChannelCatalog(request.Context(), procurement.Actor{
		CustomerID: actor.CustomerID, Role: actor.Role,
	}, request.PathValue("channel"))
	if err != nil {
		// Здесь общая заглушка «не удалось выполнить операцию» бесполезна:
		// причина почти всегда в правах токена или в закрытом методе
		// площадки, и без неё человек видит только то, что кнопка не
		// сработала. Текст приходит от площадки уже подрезанным и ключей
		// не содержит — они уходят заголовком, а не в адресе.
		if errors.Is(err, procurement.ErrInvalidInput) {
			handlers.failed(response, "sync procurement channel catalog", err)
			return
		}
		handlers.logger.Error("sync procurement channel catalog failed", "error", err)
		message := err.Error()
		if runes := []rune(message); len(runes) > 300 {
			message = string(runes[:300]) + "…"
		}
		writeJSON(response, http.StatusBadGateway, errorResponse{Error: message})
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"link": result})
}

func (handlers procurementHandlers) updateExclusion(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.admin.authorize(response, request, admin.PermissionProcurementEdit)
	if !ok {
		return
	}
	var input procurement.ExclusionUpdate
	if decodeJSON(request, &input) != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректный запрос"})
		return
	}
	if err := handlers.service.SetExclusion(request.Context(), procurement.Actor{CustomerID: actor.CustomerID, Role: actor.Role}, input); err != nil {
		handlers.failed(response, "update procurement exclusion", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"ok": true})
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
		Kind     string   `json:"kind"`
		Channels []string `json:"channels"`
	}
	if decodeJSON(request, &input) != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректный тип документа"})
		return
	}
	item, err := handlers.service.PrepareBatch(request.Context(), procurement.Actor{CustomerID: actor.CustomerID, Role: actor.Role}, orderID, input.Kind, input.Channels)
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

func (handlers procurementHandlers) retryBatch(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.admin.authorize(response, request, admin.PermissionProcurementEdit)
	if !ok {
		return
	}
	batchID, ok := pathID(response, request)
	if !ok {
		return
	}
	item, err := handlers.service.RetryBatch(request.Context(), procurement.Actor{CustomerID: actor.CustomerID, Role: actor.Role}, batchID)
	if err != nil {
		handlers.failed(response, "retry procurement batch", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"batch": item})
}

func (handlers procurementHandlers) failed(response http.ResponseWriter, operation string, err error) {
	status := http.StatusServiceUnavailable
	message := "Не удалось выполнить операцию"
	var userFacing *procurement.UserFacingError
	if errors.As(err, &userFacing) {
		status, message = http.StatusUnprocessableEntity, userFacing.Message
	}
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
	if errors.Is(err, procurement.ErrSupplierInUse) {
		status, message = http.StatusConflict, "Поставщика нельзя удалить: с ним уже есть закупки или документы"
	}
	if errors.Is(err, procurement.ErrOrderNotCancelled) {
		status, message = http.StatusConflict, "Сначала отмените закупку, затем её можно удалить"
	}
	handlers.logger.Error(operation+" failed", "error", err)
	writeJSON(response, status, errorResponse{Error: message})
}
