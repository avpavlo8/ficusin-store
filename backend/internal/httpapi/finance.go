package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
)

type financeRepository interface {
	FinanceOverview(context.Context) (admin.FinanceOverview, error)
	PreviewFinanceImport(context.Context, admin.Actor, string, string, string, []byte) (admin.FinanceImport, error)
	ConfirmFinanceImport(context.Context, admin.Actor, int64) (admin.FinanceImport, error)
	ClassifyFinanceTransaction(context.Context, admin.Actor, int64, admin.FinanceClassification) (admin.FinanceTransaction, error)
	CreateFinanceCash(context.Context, admin.Actor, admin.FinanceCashInput) (admin.FinanceCashEntry, error)
	ReconcileFinanceCash(context.Context, admin.Actor, admin.FinanceReconciliation) error
	FinancePnL(context.Context, string, string) (admin.PnLReport, error)
	SaveFinanceTax(context.Context, admin.Actor, admin.TaxPeriodInput) error
	CreateMarketplaceAdjustment(context.Context, admin.Actor, admin.MarketplaceAdjustmentInput) error
	SupplierAccount(context.Context) (admin.SupplierAccountOverview, error)
	CreateSupplierAccountOperation(context.Context, admin.Actor, admin.SupplierAccountInput) (admin.SupplierAccountOperation, error)
	ReconcileSupplierAccount(context.Context, admin.Actor, admin.SupplierReconciliationInput) error
}

func (handlers adminHandlers) financeRepository() (financeRepository, bool) {
	repository, ok := handlers.repository.(financeRepository)
	return repository, ok
}

func (handlers adminHandlers) financeOverview(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := handlers.authorize(w, r, admin.PermissionFinanceRead); !ok {
		return
	}
	repository, ok := handlers.financeRepository()
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "Финансовый учёт не подключён"})
		return
	}
	result, err := repository.FinanceOverview(r.Context())
	if err != nil {
		handlers.failed(w, "finance overview", err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (handlers adminHandlers) financePnL(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := handlers.authorize(w, r, admin.PermissionFinanceRead); !ok {
		return
	}
	repository, ok := handlers.financeRepository()
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "Финансовый учёт не подключён"})
		return
	}
	result, err := repository.FinancePnL(r.Context(), r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		handlers.financeError(w, "finance pnl", err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (handlers adminHandlers) saveFinanceTax(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := handlers.authorize(w, r, admin.PermissionFinanceEdit)
	if !ok {
		return
	}
	repository, ok := handlers.financeRepository()
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "Финансовый учёт не подключён"})
		return
	}
	var body admin.TaxPeriodInput
	if decodeJSON(r, &body) != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "Проверьте налог"})
		return
	}
	if err := repository.SaveFinanceTax(r.Context(), actor, body); err != nil {
		handlers.financeError(w, "finance tax", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
func (handlers adminHandlers) createMarketplaceAdjustment(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := handlers.authorize(w, r, admin.PermissionFinanceEdit)
	if !ok {
		return
	}
	repository, ok := handlers.financeRepository()
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "Финансовый учёт не подключён"})
		return
	}
	var body admin.MarketplaceAdjustmentInput
	if decodeJSON(r, &body) != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "Проверьте строку отчёта"})
		return
	}
	if err := repository.CreateMarketplaceAdjustment(r.Context(), actor, body); err != nil {
		handlers.financeError(w, "finance marketplace adjustment", err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]bool{"ok": true})
}
func (handlers adminHandlers) supplierAccount(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := handlers.authorize(w, r, admin.PermissionFinanceRead); !ok {
		return
	}
	repository, ok := handlers.financeRepository()
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "Финансовый учёт не подключён"})
		return
	}
	result, err := repository.SupplierAccount(r.Context())
	if err != nil {
		handlers.financeError(w, "supplier account", err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (handlers adminHandlers) createSupplierAccountOperation(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := handlers.authorize(w, r, admin.PermissionFinanceEdit)
	if !ok {
		return
	}
	repository, ok := handlers.financeRepository()
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "Финансовый учёт не подключён"})
		return
	}
	var body admin.SupplierAccountInput
	if decodeJSON(r, &body) != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "Проверьте операцию"})
		return
	}
	item, err := repository.CreateSupplierAccountOperation(r.Context(), actor, body)
	if err != nil {
		handlers.financeError(w, "supplier operation", err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"operation": item})
}
func (handlers adminHandlers) reconcileSupplierAccount(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := handlers.authorize(w, r, admin.PermissionFinanceEdit)
	if !ok {
		return
	}
	repository, ok := handlers.financeRepository()
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "Финансовый учёт не подключён"})
		return
	}
	var body admin.SupplierReconciliationInput
	if decodeJSON(r, &body) != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "Проверьте сверку"})
		return
	}
	if err := repository.ReconcileSupplierAccount(r.Context(), actor, body); err != nil {
		handlers.financeError(w, "supplier reconciliation", err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]bool{"ok": true})
}

func (handlers adminHandlers) previewFinanceImport(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := handlers.authorize(w, r, admin.PermissionFinanceEdit)
	if !ok {
		return
	}
	repository, ok := handlers.financeRepository()
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "Финансовый учёт не подключён"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 26<<20)
	if err := r.ParseMultipartForm(26 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "Файл больше 25 МБ или повреждён"})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "Выберите выписку"})
		return
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, (25<<20)+1))
	if err != nil || len(content) > 25<<20 {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "Не удалось прочитать файл"})
		return
	}
	item, err := repository.PreviewFinanceImport(r.Context(), actor, r.FormValue("bank"), r.FormValue("accountNumber"), header.Filename, content)
	if err != nil {
		handlers.financeError(w, "finance preview", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"import": item})
}

func (handlers adminHandlers) confirmFinanceImport(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := handlers.authorize(w, r, admin.PermissionFinanceEdit)
	if !ok {
		return
	}
	repository, ok := handlers.financeRepository()
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "Финансовый учёт не подключён"})
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	item, err := repository.ConfirmFinanceImport(r.Context(), actor, id)
	if err != nil {
		handlers.financeError(w, "finance confirm", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"import": item})
}
func (handlers adminHandlers) classifyFinanceTransaction(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := handlers.authorize(w, r, admin.PermissionFinanceEdit)
	if !ok {
		return
	}
	repository, ok := handlers.financeRepository()
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "Финансовый учёт не подключён"})
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body admin.FinanceClassification
	if decodeJSON(r, &body) != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "Проверьте статью"})
		return
	}
	item, err := repository.ClassifyFinanceTransaction(r.Context(), actor, id, body)
	if err != nil {
		handlers.financeError(w, "finance classify", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"transaction": item})
}
func (handlers adminHandlers) createFinanceCash(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := handlers.authorize(w, r, admin.PermissionFinanceEdit)
	if !ok {
		return
	}
	repository, ok := handlers.financeRepository()
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "Финансовый учёт не подключён"})
		return
	}
	var body admin.FinanceCashInput
	if decodeJSON(r, &body) != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "Проверьте операцию"})
		return
	}
	item, err := repository.CreateFinanceCash(r.Context(), actor, body)
	if err != nil {
		handlers.financeError(w, "finance cash", err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"entry": item})
}
func (handlers adminHandlers) reconcileFinanceCash(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := handlers.authorize(w, r, admin.PermissionFinanceEdit)
	if !ok {
		return
	}
	repository, ok := handlers.financeRepository()
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "Финансовый учёт не подключён"})
		return
	}
	var body admin.FinanceReconciliation
	if decodeJSON(r, &body) != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "Проверьте сверку"})
		return
	}
	if err := repository.ReconcileFinanceCash(r.Context(), actor, body); err != nil {
		handlers.financeError(w, "finance cash reconcile", err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]bool{"ok": true})
}
func (handlers adminHandlers) financeError(w http.ResponseWriter, operation string, err error) {
	if errors.Is(err, admin.ErrForbidden) {
		writeJSON(w, http.StatusForbidden, errorResponse{Error: "Раздел доступен владельцу"})
		return
	}
	handlers.logger.Warn(operation, "error", err)
	message := strings.TrimSpace(err.Error())
	if message == "" {
		message = "Не удалось выполнить операцию"
	}
	writeJSON(w, http.StatusBadRequest, errorResponse{Error: message})
}
