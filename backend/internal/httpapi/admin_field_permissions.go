package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
)

func validateManagerProductFields(response http.ResponseWriter, actor admin.Actor, fields map[string]json.RawMessage) bool {
	if admin.Can(actor.Role, admin.PermissionProductsManage) {
		if actor.Role != admin.RoleOwner {
			for key := range fields {
				if strings.EqualFold(key, "stock") || strings.EqualFold(key, "sabyFields") {
					writeJSON(response, http.StatusForbidden, errorResponse{Error: "Остатки обновляются из СБИС"})
					return false
				}
			}
		}
		return true
	}
	allowed := map[string]bool{}
	for _, key := range []string{"variants", "name", "latinName", "shortDescription", "description", "careInstructions", "status", "featured", "catalogSection", "plantKind", "lightLevel", "watering", "heightClass", "careLevel", "placement", "petSafety", "growthHabit", "categoryId", "clearCategory", "passport", "importantWarnings", "attributes"} {
		allowed[key] = true
	}
	for key := range fields {
		if !allowed[key] {
			writeJSON(response, http.StatusForbidden, errorResponse{Error: "Запрос содержит поля, доступные только владельцу. Ничего не сохранено."})
			return false
		}
	}
	var variants []map[string]json.RawMessage
	if raw, ok := fields["variants"]; ok {
		if json.Unmarshal(raw, &variants) != nil {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректные размеры"})
			return false
		}
		for _, variant := range variants {
			for key := range variant {
				if key != "id" && key != "label" && key != "attributes" {
					writeJSON(response, http.StatusForbidden, errorResponse{Error: "Цена, активация и связи размера доступны владельцу. Ничего не сохранено."})
					return false
				}
			}
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

// Reject explicit manual inventory fields even when a client sends null/zero.
func decodeEditorVariantInput(w http.ResponseWriter, r *http.Request, actor admin.Actor) (admin.VariantInput, bool) {
	var fields map[string]json.RawMessage
	if decodeJSON(r, &fields) != nil || fields == nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "Некорректные данные"})
		return admin.VariantInput{}, false
	}
	if actor.Role != admin.RoleOwner {
		for key := range fields {
			if strings.EqualFold(key, "stock") {
				writeJSON(w, http.StatusForbidden, errorResponse{Error: "Остатки обновляются из СБИС"})
				return admin.VariantInput{}, false
			}
		}
	}
	raw, _ := json.Marshal(fields)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var input admin.VariantInput
	if decoder.Decode(&input) != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "Некорректные данные"})
		return input, false
	}
	return input, true
}
