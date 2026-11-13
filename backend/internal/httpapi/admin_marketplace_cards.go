package httpapi

import (
	"context"
	"encoding/json"
	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	ce "github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type editorArticleCardSource interface {
	ReadEditorCardByArticle(context.Context, string, string) (*ce.RemoteCard, error)
}
type editorCardSource interface {
	ReadEditorCard(context.Context, string, string) (ce.RemoteCard, error)
}

func editorMarketplaceCardsHandler(h adminHandlers, source editorReferenceSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := h.authorize(w, r, admin.PermissionProductsRead); !ok {
			return
		}
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		variant, e := strconv.ParseInt(r.URL.Query().Get("variantId"), 10, 64)
		if e != nil || variant <= 0 {
			writeJSON(w, 400, errorResponse{Error: "Укажите вариант товара"})
			return
		}
		repo, ok := h.repository.(editorPlanRepository)
		if !ok {
			writeJSON(w, 503, errorResponse{Error: "Карточки площадок недоступны"})
			return
		}
		view, e := repo.ReadEditorPlan(r.Context(), id)
		if e != nil {
			h.failed(w, "read marketplace links", e)
			return
		}
		links, exists := view.Links[variant]
		if !exists {
			writeJSON(w, 404, errorResponse{Error: "Вариант не принадлежит товару"})
			return
		}
		if channel := r.URL.Query().Get("channel"); channel != "" {
			article := strings.TrimSpace(r.URL.Query().Get("article"))
			if (channel != "wb" && channel != "ozon") || article == "" || len(article) > 500 {
				writeJSON(w, 400, errorResponse{Error: "Укажите площадку и артикул продавца"})
				return
			}
			reader, ok := source.(editorArticleCardSource)
			if !ok {
				writeJSON(w, 503, errorResponse{Error: "Подключение площадки недоступно"})
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
			defer cancel()
			card, err := reader.ReadEditorCardByArticle(ctx, channel, article)
			if err != nil {
				h.failed(w, "read marketplace article", err)
				return
			}
			if card != nil && card.Card.Channel.Category.Name == "" {
				raw, _ := source.ReadEditorReference(ctx, channel, "categories", 0, 0, 0, "")
				card.Card.Channel.Category.Name = editorCategoryName(raw, channel, card.Card.Channel.Category)
			}
			writeJSON(w, 200, map[string]any{"card": card, "found": card != nil, "publicationMode": view.PublicationMode})
			return
		}
		cards := map[string]ce.RemoteCard{}
		failures := map[string]string{}
		reader, available := source.(editorCardSource)
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		type answer struct {
			channel string
			card    ce.RemoteCard
			err     error
		}
		pending := make(chan answer, 2)
		count := 0
		for channel, identifier := range links {
			// A receipt from another provider must never be sent to the WB/Ozon
			// reader or reported as a marketplace credential failure.
			if channel != "wb" && channel != "ozon" {
				continue
			}
			if identifier == "!ambiguous" {
				failures[channel] = "Найдено несколько связей. Уточните соответствие карточки."
				continue
			}
			if !available {
				failures[channel] = "Подключение площадки недоступно"
				continue
			}
			count++
			go func(ch, code string) {
				card, err := reader.ReadEditorCard(ctx, ch, code)
				pending <- answer{ch, card, err}
			}(channel, identifier)
		}
		for i := 0; i < count; i++ {
			result := <-pending
			if result.err != nil {
				failures[result.channel] = result.err.Error()
				continue
			}
			cards[result.channel] = result.card
		}
		for channel, card := range cards {
			if card.Card.Channel.Category.Name == "" {
				raw, _ := repo.CachedEditorReference(ctx, channel, "categories", "0:0:0:")
				if len(raw) == 0 && source != nil {
					raw, e = source.ReadEditorReference(ctx, channel, "categories", 0, 0, 0, "")
					if e == nil {
						_ = repo.CacheEditorReference(ctx, channel, "categories", "0:0:0:", raw)
					}
				}
				card.Card.Channel.Category.Name = editorCategoryName(raw, channel, card.Card.Channel.Category)
				if card.Card.Channel.Category.Name == "" {
					card.Card.Channel.Category.Name = card.Card.Channel.Category.ID
				}
				cards[channel] = card
			}
		}
		writeJSON(w, 200, map[string]any{"cards": cards, "errors": failures, "publicationMode": view.PublicationMode})
	}
}
func editorCategoryName(raw []byte, channel string, category ce.Category) string {
	var tree struct {
		Result []map[string]any `json:"result"`
		Data   []map[string]any `json:"data"`
	}
	if json.Unmarshal(raw, &tree) != nil {
		return ""
	}
	var walk func([]map[string]any, []string, string) string
	walk = func(nodes []map[string]any, path []string, parent string) string {
		for _, node := range nodes {
			if channel == "wb" {
				if jsonID(node["subjectID"]) == category.ID {
					return strings.Trim(strings.Join([]string{stringField(node, "parentName"), stringField(node, "subjectName")}, " / "), " / ")
				}
				continue
			}
			cat := jsonID(node["description_category_id"])
			if cat == "" || cat == "0" {
				cat = parent
			}
			title := stringField(node, "category_name")
			if title == "" {
				title = stringField(node, "type_name")
			}
			next := append(append([]string{}, path...), title)
			if cat == category.ID && jsonID(node["type_id"]) == strconv.FormatInt(category.TypeID, 10) {
				return strings.Join(next, " / ")
			}
			if children, ok := node["children"].([]any); ok {
				typed := []map[string]any{}
				for _, child := range children {
					if v, ok := child.(map[string]any); ok {
						typed = append(typed, v)
					}
				}
				if name := walk(typed, next, cat); name != "" {
					return name
				}
			}
		}
		return ""
	}
	return walk(append(tree.Result, tree.Data...), nil, "")
}
func jsonID(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatInt(int64(v), 10)
	}
	return ""
}
func stringField(value map[string]any, key string) string { s, _ := value[key].(string); return s }
