package httpapi

import (
	"net/http"
	"strconv"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/procurement"
)

func (handlers procurementHandlers) previewUnitPrice(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := handlers.admin.authorize(w, r, admin.PermissionProcurementRead); !ok {
		return
	}
	var input procurement.UnitPriceInput
	if decodeJSON(r, &input) != nil {
		handlers.failed(w, "unit price input", procurement.ErrInvalidInput)
		return
	}
	service := handlers.service
	if service == nil {
		handlers.failed(w, "unit price unavailable", procurement.ErrInvalidInput)
		return
	}
	result, err := service.PreviewUnitPrice(r.Context(), input)
	if err != nil {
		handlers.failed(w, "unit price preview", err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (handlers procurementHandlers) pricingExample(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := handlers.admin.authorize(w, r, admin.PermissionIntegrationsEdit); !ok {
		return
	}
	var input procurement.PricingSettings
	if decodeJSON(r, &input) != nil {
		handlers.failed(w, "pricing example input", procurement.ErrInvalidInput)
		return
	}
	service := handlers.service
	if service == nil {
		handlers.failed(w, "pricing example unavailable", procurement.ErrInvalidInput)
		return
	}
	result, err := service.PricingExample(r.Context(), input)
	if err != nil {
		handlers.failed(w, "pricing example", err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (handlers procurementHandlers) updateSupplier(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := handlers.admin.authorize(w, r, admin.PermissionProcurementEdit)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var input procurement.SupplierUpdate
	if decodeJSON(r, &input) != nil {
		handlers.failed(w, "decode supplier", procurement.ErrInvalidInput)
		return
	}
	service := handlers.service
	if service == nil {
		handlers.failed(w, "update supplier unavailable", procurement.ErrInvalidInput)
		return
	}
	item, err := service.UpdateSupplier(r.Context(), procurement.Actor{CustomerID: actor.CustomerID, Role: actor.Role}, id, input)
	if err != nil {
		handlers.failed(w, "update procurement supplier", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"supplier": item})
}

func (handlers procurementHandlers) documentPDF(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := handlers.admin.authorize(w, r, admin.PermissionProcurementRead); !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	service := handlers.service
	if service == nil {
		handlers.failed(w, "source PDF unavailable", procurement.ErrInvalidInput)
		return
	}
	content, err := service.DocumentPDF(r.Context(), id)
	if err != nil {
		handlers.failed(w, "read source PDF", err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `inline; filename="procurement-`+strconv.FormatInt(id, 10)+`.pdf"`)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}
