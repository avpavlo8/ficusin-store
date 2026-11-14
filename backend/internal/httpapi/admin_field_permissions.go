package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
)

func validateManagerProductFields(response http.ResponseWriter, actor admin.Actor, fields map[string]json.RawMessage) bool {
	if actor.Role == admin.RoleOwner {
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
