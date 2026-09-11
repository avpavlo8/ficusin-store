package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
)

func validateManagerProductFields(response http.ResponseWriter, actor admin.Actor, fields map[string]json.RawMessage) bool {
	if actor.Role == admin.RoleOwner {
		return true
	}
	allowed := map[string]bool{}
	for _, key := range []string{"name", "latinName", "shortDescription", "description", "careInstructions", "status", "featured", "catalogSection", "plantKind", "lightLevel", "watering", "heightClass", "careLevel", "placement", "petSafety", "growthHabit", "variantLabel", "heightCm", "potDiameterCm", "packageLengthCm", "packageWidthCm", "packageHeightCm", "packageWeightGrams", "categoryId", "clearCategory", "passport", "importantWarnings", "attributes"} {
		allowed[key] = true
	}
	for key := range fields {
		if !allowed[key] {
			writeJSON(response, http.StatusForbidden, errorResponse{Error: "Запрос содержит поля, доступные только владельцу. Ничего не сохранено."})
			return false
		}
	}
	var status string
	_ = json.Unmarshal(fields["status"], &status)
	if status == "archived" {
		writeJSON(response, http.StatusForbidden, errorResponse{Error: "Архивирование доступно владельцу. Ничего не сохранено."})
		return false
	}
	return true
}

func managerVariantContent(handlers adminHandlers, response http.ResponseWriter, request *http.Request, actor admin.Actor, id int64) {
	var fields map[string]json.RawMessage
	if decodeJSON(request, &fields) != nil || fields == nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректные данные"})
		return
	}
	for key := range fields {
		if key != "label" && key != "attributes" {
			writeJSON(response, http.StatusForbidden, errorResponse{Error: "Цены, остатки и связи SKU доступны владельцу. Ничего не сохранено."})
			return
		}
	}
	var input admin.VariantContentUpdate
	raw, _ := json.Marshal(fields)
	if json.Unmarshal(raw, &input) != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректные данные"})
		return
	}
	repository, ok := handlers.repository.(interface {
		UpdateVariantContent(context.Context, admin.Actor, int64, admin.VariantContentUpdate) (admin.AdminVariant, error)
	})
	if !ok {
		writeJSON(response, http.StatusNotImplemented, errorResponse{Error: "Редактирование характеристик недоступно"})
		return
	}
	item, err := repository.UpdateVariantContent(request.Context(), actor, id, input)
	if err != nil {
		handlers.failed(response, "update variant content", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"variant": item})
}
