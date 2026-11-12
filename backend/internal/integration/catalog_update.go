package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	ce "github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
	"strconv"
)

// Hydrate updates from the existing remote card. Missing creation-form fields
// must not erase remote characteristics, barcode, category, sizes or documents.
func (p *CatalogPublisher) hydrateCatalogUpdate(ctx context.Context, ch string, r ce.Request) (ce.Request, ce.RemoteIDs, error) {
	ids, e := p.lookup(ctx, ch, r.ExistingID, false)
	if e != nil {
		return r, ids, e
	}
	if ids.ProductID == "" || ids.ProductID == "0" {
		return r, ids, errors.New("связанная карточка не найдена на площадке")
	}
	var card map[string]any
	if ch == "wb" {
		var out struct {
			Cards []map[string]any `json:"cards"`
		}
		e = p.read(ctx, ch, "/content/v2/get/cards/list", map[string]any{"settings": map[string]any{"cursor": map[string]any{"limit": 100}, "filter": map[string]any{"textSearch": ids.ProductID, "withPhoto": -1}}}, &out)
		if e != nil {
			return r, ids, e
		}
		for _, item := range out.Cards {
			if numericID(item["nmID"]) == ids.ProductID {
				card = item
				break
			}
		}
	} else {
		var out struct {
			Result []map[string]any `json:"result"`
		}
		e = p.read(ctx, ch, "/v4/product/info/attributes", map[string]any{"filter": map[string]any{"offer_id": []string{ids.OfferID}, "visibility": "ALL"}, "limit": 100, "last_id": ""}, &out)
		if e != nil {
			return r, ids, e
		}
		for _, item := range out.Result {
			if item["offer_id"] == ids.OfferID {
				card = item
				break
			}
		}
	}
	if card == nil {
		return r, ids, errors.New("не удалось получить характеристики связанной карточки")
	}
	if r.Name == "" {
		key := "name"
		if ch == "wb" {
			key = "title"
		}
		r.Name, _ = card[key].(string)
	}
	if r.Description == "" {
		r.Description, _ = card["description"].(string)
	}
	configuredCategory := r.Channel.Category.ID != ""
	category := ce.Category{ID: numericID(card["description_category_id"]), TypeID: numberID(card["type_id"])}
	if ch == "wb" {
		category.ID = numericID(card["subjectID"])
		category.Name, _ = card["subjectName"].(string)
	}
	if r.Channel.Category.ID != "" {
		if ch == "wb" && r.Channel.Category.ID != category.ID {
			return r, ids, errors.New("категория созданной карточки Wildberries отличается; проверьте связь")
		}
		category = r.Channel.Category
	}
	cat, _ := strconv.ParseInt(category.ID, 10, 64)
	schemaRaw, e := p.Executor.ReadEditorReference(ctx, ch, "attributes", cat, category.TypeID, 0, "")
	if e != nil {
		return r, ids, e
	}
	var schema struct {
		Result []ce.Attribute `json:"result"`
		Data   []ce.Attribute `json:"data"`
	}
	if e = json.Unmarshal(schemaRaw, &schema); e != nil {
		return r, ids, e
	}
	old := map[string]any{}
	if ch == "wb" {
		if attrs, ok := card["characteristics"].([]any); ok {
			for _, raw := range attrs {
				a, _ := raw.(map[string]any)
				vv := []ce.Value{}
				switch v := a["value"].(type) {
				case []any:
					for _, item := range v {
						vv = append(vv, ce.Value{Value: fmt.Sprint(item)})
					}
				default:
					if v != nil {
						vv = append(vv, ce.Value{Value: fmt.Sprint(v)})
					}
				}
				old[numericID(a["id"])] = vv
			}
		}
	} else {
		take := func(raw any) {
			if attrs, ok := raw.([]any); ok {
				for _, item := range attrs {
					a, _ := item.(map[string]any)
					values := []ce.Value{}
					if vv, ok := a["values"].([]any); ok {
						for _, item := range vv {
							v, _ := item.(map[string]any)
							values = append(values, ce.Value{Value: fmt.Sprint(v["value"]), DictionaryID: numberID(v["dictionary_value_id"])})
						}
					}
					old[numericID(a["id"])] = values
				}
			}
		}
		take(card["attributes"])
		if groups, ok := card["complex_attributes"].([]any); ok {
			for _, group := range groups {
				g, _ := group.(map[string]any)
				take(g["attributes"])
			}
		}
	}
	configuredSchema := configuredCategory
	r.Schema = append(schema.Result, schema.Data...)
	for key, value := range r.Channel.Attributes {
		if len(ce.Values(value)) > 0 {
			old[key] = value
		}
	}
	r.Channel.Attributes = old
	r.Channel.Category = category
	r.Channel.Mode = "create"
	r.Channel.Enabled = true
	r.Marketplace.Article = ids.OfferID
	r.Channel.Article = &ids.OfferID
	r.Barcode = ids.Barcode
	for _, a := range r.Schema {
		values := ce.Values(old[strconv.FormatInt(a.Key(), 10)])
		if len(values) == 0 {
			if ch == "ozon" && a.DictionaryID > 0 {
				switch a.Role(ch) {
				case "brand":
					if r.Marketplace.BrandID <= 0 {
						r.Marketplace.Brand = ""
					}
				case "tnved":
					if r.Marketplace.TNVEDID <= 0 {
						r.Marketplace.TNVED = ""
					}
				}
			}
			continue
		}
		v := values[0]
		switch a.Role(ch) {
		case "description":
			if r.Description == "" {
				r.Description = v.Value
			}
		case "brand":
			if r.Marketplace.Brand == "" || ch == "ozon" && a.DictionaryID > 0 && r.Marketplace.BrandID <= 0 {
				r.Marketplace.Brand = v.Value
				r.Marketplace.BrandID = v.DictionaryID
			}
		case "tnved":
			if r.Marketplace.TNVED == "" || ch == "ozon" && a.DictionaryID > 0 && r.Marketplace.TNVEDID <= 0 {
				r.Marketplace.TNVED = v.Value
				r.Marketplace.TNVEDID = v.DictionaryID
			}
		case "model":
			if r.Marketplace.Model == "" {
				r.Marketplace.Model = v.Value
			}
		case "units":
			if !configuredSchema || r.Marketplace.Units <= 0 {
				r.Marketplace.Units, _ = strconv.Atoi(v.Value)
			}
		}
	}
	if r.Marketplace.Brand == "" {
		if brand, ok := card["brand"].(string); ok {
			r.Marketplace.Brand = brand
		}
	}
	if ch == "ozon" {
		r.Marketplace.Ozon.Category = category
	}
	pack := r.Packaging
	if pack.Length <= 0 || pack.Width <= 0 || pack.Height <= 0 || pack.Weight <= 0 {
		if ch == "wb" {
			d, _ := card["dimensions"].(map[string]any)
			pack.Length = number(d["length"])
			pack.Width = number(d["width"])
			pack.Height = number(d["height"])
			pack.Weight = number(d["weightBrutto"]) * 1000
		} else {
			scale := .1
			if card["dimension_unit"] == "cm" {
				scale = 1
			}
			pack.Length = number(card["depth"]) * scale
			pack.Width = number(card["width"]) * scale
			pack.Height = number(card["height"]) * scale
			pack.Weight = number(card["weight"])
			if card["weight_unit"] == "kg" {
				pack.Weight *= 1000
			}
		}
		r.Packaging = pack
	}
	if len(r.Images) == 0 {
		key := "images"
		if ch == "wb" {
			key = "photos"
		}
		if images, ok := card[key].([]any); ok {
			for _, image := range images {
				switch v := image.(type) {
				case string:
					r.Images = append(r.Images, v)
				case map[string]any:
					for _, name := range []string{"big", "url", "file_name"} {
						if link, ok := v[name].(string); ok && link != "" {
							r.Images = append(r.Images, link)
							break
						}
					}
				}
			}
		}
	}
	if ch == "wb" {
		if _, err := catalogPrice(r.WBPrice); err != nil {
			prices, err := p.Executor.readWBProductPrices(ctx, ids.ProductID)
			if err != nil {
				return r, ids, err
			}
			if len(prices) > 0 {
				r.WBPrice = strconv.FormatFloat(prices[0], 'f', 2, 64)
			}
		}
	}
	if ch == "ozon" {
		if _, err := catalogPrice(r.OzonPrice); err != nil {
			var prices struct {
				Items []struct {
					OfferID string `json:"offer_id"`
					Price   struct {
						Price marketplaceNumber `json:"price"`
					} `json:"price"`
				} `json:"items"`
			}
			if err = p.read(ctx, ch, "/v5/product/info/prices", map[string]any{"filter": map[string]any{"offer_id": []string{ids.OfferID}, "visibility": "ALL"}, "limit": 100}, &prices); err != nil {
				return r, ids, err
			}
			for _, item := range prices.Items {
				if item.OfferID == ids.OfferID {
					r.OzonPrice = strconv.FormatFloat(float64(item.Price.Price), 'f', 2, 64)
				}
			}
		}
	}
	r.Action = "updating"
	return r, ids, nil
}
func number(raw any) float64 {
	switch v := raw.(type) {
	case float64:
		return v
	case string:
		n, _ := strconv.ParseFloat(v, 64)
		return n
	}
	return 0
}
func numberID(raw any) int64 { return int64(number(raw)) }
func numericID(raw any) string {
	if v, ok := raw.(string); ok {
		return v
	}
	return strconv.FormatInt(numberID(raw), 10)
}

// ReadEditorCard hydrates the editor using catalogue reads only. It has no
// publication side effect and does not change site-owned photos or stock.
func (e *MarketplaceExecutor) ReadEditorCard(ctx context.Context, channel, identifier string) (ce.RemoteCard, error) {
	if !e.Configured(channel) {
		return ce.RemoteCard{}, errors.New("подключение площадки не настроено")
	}
	r, ids, err := (&CatalogPublisher{Executor: e}).hydrateCatalogUpdate(ctx, channel, ce.Request{ExistingID: identifier})
	return ce.RemoteCard{Card: r, ExternalIDs: ids}, err
}

// ReadEditorCardByArticle matches seller identity exactly, including numeric articles.
func (e *MarketplaceExecutor) ReadEditorCardByArticle(ctx context.Context, channel, article string) (*ce.RemoteCard, error) {
	p := &CatalogPublisher{Executor: e}
	ids, err := p.lookup(ctx, channel, article, true)
	if err != nil {
		return nil, err
	}
	if ids.ProductID == "" || ids.ProductID == "0" {
		return nil, nil
	}
	identifier := ids.OfferID
	if channel == "wb" {
		identifier = ids.ProductID
	}
	card, err := e.ReadEditorCard(ctx, channel, identifier)
	return &card, err
}
