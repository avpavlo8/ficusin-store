package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Only read-only vendor methods are reachable through this adapter.
func (e *MarketplaceExecutor) ReadEditorReference(ctx context.Context, ch, kind string, category, typeID, attr int64, q string, cursors ...string) (json.RawMessage, error) {
	if kind == "existing" {
		if !e.Configured(ch) {
			return nil, fmt.Errorf("Подключение %s не настроено", ch)
		}
		card, err := e.LookupEditorCard(ctx, ch, q)
		if err != nil {
			return nil, err
		}
		if card.ProductID == "" || card.ProductID == "0" {
			return nil, fmt.Errorf("Товар не найден в кабинете продавца")
		}
		return json.Marshal(map[string]any{"card": card})
	}
	var raw json.RawMessage
	path := ""
	payload := map[string]any{"language": "RU"}
	headers := map[string]string{}
	if ch == "ozon" {
		if e.ozonClientID == "" || e.ozonAPIKey == "" {
			return nil, fmt.Errorf("Подключение справочников Ozon не настроено")
		}
		headers = map[string]string{"Client-Id": e.ozonClientID, "Api-Key": e.ozonAPIKey}
		switch kind {
		case "categories":
			path = "/v1/description-category/tree"
		case "attributes":
			path = "/v1/description-category/attribute"
			payload["description_category_id"] = category
			payload["type_id"] = typeID
		case "values":
			path = "/v1/description-category/attribute/values"
			payload["description_category_id"] = category
			payload["type_id"] = typeID
			payload["attribute_id"] = attr
			payload["limit"] = 100
			payload["last_value_id"] = 0
			if len(cursors) > 0 {
				payload["last_value_id"], _ = strconv.ParseInt(cursors[0], 10, 64)
			}
			if len([]rune(q)) >= 2 {
				path += "/search"
				delete(payload, "last_value_id")
				payload["value"] = q
			}
		default:
			return nil, fmt.Errorf("Неизвестный справочник")
		}
		err := e.requestRead(ctx, http.MethodPost, e.ozonBase+path, payload, headers, &raw)
		return raw, err
	}
	if ch == "wb" {
		if e.wbToken == "" {
			return nil, fmt.Errorf("Подключение справочников Wildberries не настроено")
		}
		switch kind {
		case "categories":
			path = "/content/v2/object/all?locale=ru&limit=1000&offset=0&name=" + url.QueryEscape(q)
		case "attributes":
			path = fmt.Sprintf("/content/v2/object/charcs/%d?locale=ru", category)
		case "brand":
			cursor := "0"
			if len(cursors) > 0 && cursors[0] != "" {
				cursor = cursors[0]
			}
			path = fmt.Sprintf("/api/content/v1/brands?subjectId=%d&next=%s", category, url.QueryEscape(cursor))
		case "tnved":
			path = fmt.Sprintf("/content/v2/directory/tnved?locale=ru&subjectID=%d&search=%s", category, url.QueryEscape(q))
		case "values":
			schemaRaw, err := e.ReadEditorReference(ctx, "wb", "attributes", category, 0, 0, "")
			if err != nil {
				return nil, err
			}
			var schema struct {
				Data []struct {
					ID   int64  `json:"charcID"`
					Name string `json:"name"`
				} `json:"data"`
			}
			if err = json.Unmarshal(schemaRaw, &schema); err != nil {
				return nil, err
			}
			for _, a := range schema.Data {
				if a.ID == attr {
					name := strings.ToLower(a.Name)
					switch {
					case strings.Contains(name, "цвет"):
						path = "/content/v2/directory/colors"
					case strings.Contains(name, "страна"):
						path = "/content/v2/directory/countries"
					case name == "пол":
						path = "/content/v2/directory/kinds"
					case strings.Contains(name, "сезон"):
						path = "/content/v2/directory/seasons"
					case strings.Contains(name, "ндс"):
						path = "/content/v2/directory/vat"
					case strings.Contains(name, "тн") && strings.Contains(name, "вэд"):
						path = fmt.Sprintf("/content/v2/directory/tnved?subjectID=%d&search=%s", category, url.QueryEscape(q))
					}
				}
			}
			if path == "" {
				return json.RawMessage(`{"values":[],"freeText":true}`), nil
			}
		default:
			return nil, fmt.Errorf("Неизвестный справочник")
		}
		err := e.requestRead(ctx, http.MethodGet, e.wbContentBase+path, nil, map[string]string{"Authorization": e.wbToken}, &raw)
		return raw, err
	}
	return nil, fmt.Errorf("Неизвестная площадка")
}
