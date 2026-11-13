package catalogeditor

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

type Category struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	TypeID int64  `json:"typeId,omitempty"`
}
type Channel struct {
	Article     *string        `json:"article,omitempty"`
	ReplaceLink bool           `json:"replaceLink,omitempty"`
	Enabled     bool           `json:"enabled"`
	Mode        string         `json:"mode"`
	ExistingID  string         `json:"existingId"`
	Category    Category       `json:"category"`
	Attributes  map[string]any `json:"attributes,omitempty"`
}
type Marketplace struct {
	ContentFromMarketplace bool    `json:"contentFromMarketplace,omitempty"`
	Name                   string  `json:"name"`
	Article                string  `json:"article"`
	Description            string  `json:"description"`
	Brand                  string  `json:"brand"`
	BrandID                int64   `json:"brandId,omitempty"`
	Model                  string  `json:"model"`
	TNVED                  string  `json:"tnved"`
	TNVEDID                int64   `json:"tnvedId,omitempty"`
	Units                  int     `json:"units"`
	Ozon                   Channel `json:"ozon"`
	WB                     Channel `json:"wb"`
}

// SellerArticle keeps old drafts readable while each channel owns its identity.
func (m Marketplace) SellerArticle(channel string) string {
	c := m.WB
	if channel == "ozon" {
		c = m.Ozon
	}
	if c.Article != nil {
		return strings.TrimSpace(*c.Article)
	}
	return strings.TrimSpace(m.Article)
}
func (r Request) SellerArticle(channel string) string {
	if r.Channel.Article != nil {
		return strings.TrimSpace(*r.Channel.Article)
	}
	return r.Marketplace.SellerArticle(channel)
}

type Attribute struct {
	ID            int64  `json:"id"`
	CharcID       int64  `json:"charcID"`
	Name          string `json:"name"`
	Required      bool   `json:"required"`
	IsRequired    bool   `json:"is_required"`
	DictionaryID  int64  `json:"dictionary_id"`
	Type          string `json:"type"`
	CharcType     int    `json:"charcType"`
	MaxCount      int    `json:"maxCount"`
	MaxValueCount int    `json:"max_value_count"`
	ComplexID     int64  `json:"attribute_complex_id"`
}

func (a Attribute) Key() int64 {
	if a.ID != 0 {
		return a.ID
	}
	return a.CharcID
}
func (a Attribute) Mandatory() bool { return a.Required || a.IsRequired }
func (a Attribute) Numeric(channel string) bool {
	return channel == "wb" && a.CharcType == 4 || a.Type == "Integer" || a.Type == "Decimal"
}
func (a Attribute) Limit() int {
	if a.MaxValueCount > 0 {
		return a.MaxValueCount
	}
	return a.MaxCount
}
func (a Attribute) Role(channel string) string {
	n := strings.ToLower(strings.TrimSpace(a.Name))
	if channel == "ozon" && a.ID == 8229 {
		return "type"
	}
	switch {
	case strings.Contains(n, "бренд"):
		return "brand"
	case strings.Contains(n, "тн") && strings.Contains(n, "вэд"):
		return "tnved"
	case n == "название" || n == "наименование" || n == "название товара" || n == "наименование товара":
		return "name"
	case n == "аннотация" || n == "описание":
		return "description"
	case n == "артикул" || n == "артикул продавца":
		return "article"
	case n == "ндс" || n == "ставка ндс":
		return "vat"
	case channel == "ozon" && strings.Contains(n, "название модели"):
		return "model"
	case channel == "ozon" && (strings.Contains(n, "единиц в одном товаре") || strings.Contains(n, "количество в упаковке")):
		return "units"
	}
	return ""
}

type Value struct {
	Value        string `json:"value"`
	DictionaryID int64  `json:"dictionaryValueId,omitempty"`
}

func Values(raw any) []Value {
	if raw == nil {
		return nil
	}
	if s, ok := raw.(string); ok {
		if strings.TrimSpace(s) == "" {
			return nil
		}
		return []Value{{Value: strings.TrimSpace(s)}}
	}
	switch n := raw.(type) {
	case float64:
		return []Value{{Value: strconv.FormatFloat(n, 'f', -1, 64)}}
	case int:
		return []Value{{Value: strconv.Itoa(n)}}
	case bool:
		return []Value{{Value: strconv.FormatBool(n)}}
	}
	b, _ := json.Marshal(raw)
	var values []Value
	if json.Unmarshal(b, &values) == nil {
		clean := []Value{}
		for _, value := range values {
			value.Value = strings.TrimSpace(value.Value)
			if value.Value != "" {
				clean = append(clean, value)
			}
		}
		return clean
	}
	return nil
}
func AttributeValues(a Attribute, raw any, channel string) ([]Value, error) {
	values := Values(raw)
	if a.Mandatory() && len(values) == 0 {
		return nil, fmt.Errorf("заполните «%s»", a.Name)
	}
	if max := a.Limit(); max > 0 && len(values) > max {
		return nil, fmt.Errorf("«%s»: максимум значений — %d", a.Name, max)
	}
	if channel == "wb" && a.Role(channel) == "tnved" {
		for i := range values {
			code, err := TNVEDCode(values[i].Value)
			if err != nil {
				return nil, fmt.Errorf("«%s»: %w", a.Name, err)
			}
			// Ozon's dictionary label and dictionary ID are not WB values.
			values[i] = Value{Value: code}
		}
	}
	for _, v := range values {
		if strings.TrimSpace(v.Value) == "" {
			return nil, fmt.Errorf("«%s»: пустое значение", a.Name)
		}
		if channel == "ozon" && a.DictionaryID > 0 && v.DictionaryID <= 0 {
			return nil, fmt.Errorf("«%s»: выберите значение из справочника", a.Name)
		}
		if a.Numeric(channel) {
			n, e := strconv.ParseFloat(strings.ReplaceAll(v.Value, ",", "."), 64)
			if e != nil || math.IsInf(n, 0) || math.IsNaN(n) {
				return nil, fmt.Errorf("«%s»: нужно число", a.Name)
			}
			if a.Type == "Integer" && math.Trunc(n) != n {
				return nil, fmt.Errorf("«%s»: нужно целое число", a.Name)
			}
		}
		if a.Type == "Boolean" && v.Value != "true" && v.Value != "false" {
			return nil, fmt.Errorf("«%s»: выберите Да или Нет", a.Name)
		}
	}
	return values, nil
}

var tnvedLabel = regexp.MustCompile(`^([0-9]{10})(?:\s*[-–—:]\s*\S.*)?$`)

// Keep leading zeros and accept a dictionary label only when its complete
// ten-digit code is explicit. Never guess or silently shorten a longer code.
func TNVEDCode(label string) (string, error) {
	if strings.ContainsAny(label, "\r\n") {
		return "", fmt.Errorf("нужен код ТН ВЭД из 10 цифр")
	}
	match := tnvedLabel.FindStringSubmatch(strings.TrimSpace(label))
	if match == nil {
		return "", fmt.Errorf("нужен код ТН ВЭД из 10 цифр")
	}
	return match[1], nil
}
func CommonValue(a Attribute, m Marketplace, ch string, name, description string) any {
	switch a.Role(ch) {
	case "type":
		return []Value{{Value: strconv.FormatInt(m.Ozon.Category.TypeID, 10), DictionaryID: m.Ozon.Category.TypeID}}
	case "brand":
		return []Value{{Value: m.Brand, DictionaryID: m.BrandID}}
	case "tnved":
		return []Value{{Value: m.TNVED, DictionaryID: m.TNVEDID}}
	case "name":
		return name
	case "description":
		return description
	case "article":
		return m.SellerArticle(ch)
	case "model":
		return m.Model
	case "units":
		if m.Units > 0 {
			return strconv.Itoa(m.Units)
		}
		return nil
	case "vat":
		if ch == "wb" {
			return "Без НДС"
		}
		return nil // VAT is a top-level field, not a category characteristic.
	}
	return nil
}

type Packaging struct {
	Season string  `json:"season"`
	Length float64 `json:"lengthCm"`
	Width  float64 `json:"widthCm"`
	Height float64 `json:"heightCm"`
	Weight float64 `json:"weightGrams"`
}
type Request struct {
	Saby           Saby           `json:"saby,omitempty"`
	SabyID         string         `json:"sabyId,omitempty"`
	SabyUUID       string         `json:"sabyUUID,omitempty"`
	PriceMinor     int64          `json:"priceMinor,omitempty"`
	Action         string         `json:"action,omitempty"`
	ExistingID     string         `json:"existingId,omitempty"`
	SiteAttributes map[string]any `json:"attributes,omitempty"`
	VariantID      int64          `json:"variantId"`
	ProductID      int64          `json:"productId"`
	Name           string         `json:"name"`
	Description    string         `json:"description"`
	SKU            string         `json:"sku"`
	Barcode        string         `json:"barcode"`
	Images         []string       `json:"images"`
	Packaging      Packaging      `json:"packaging"`
	Marketplace    Marketplace    `json:"marketplace"`
	Channel        Channel        `json:"channel"`
	OzonPrice      string         `json:"ozonPrice"`
	WBPrice        string         `json:"wbPrice"`
	Schema         []Attribute    `json:"schema"`
}
type Saby struct {
	Category     Category `json:"category"`
	Folder       Category `json:"folder"`
	Mode         string   `json:"mode"`
	ExistingCode string   `json:"existingCode"`
}
type RemoteIDs struct {
	UUID         string `json:"uuid,omitempty"`
	ImageCount   int    `json:"imageCount,omitempty"`
	RemoteStatus string `json:"remoteStatus,omitempty"`
	ProductID    string `json:"productId,omitempty"`
	OfferID      string `json:"offerId,omitempty"`
	SKU          string `json:"sku,omitempty"`
	Barcode      string `json:"barcode,omitempty"`
}
type Progress struct {
	ReapplyPayload  bool            `json:"reapplyPayload,omitempty"`
	Native          json.RawMessage `json:"native,omitempty"`
	UpdateRequest   *Request        `json:"updateRequest,omitempty"`
	Stage           string          `json:"stage,omitempty"`
	TaskID          int64           `json:"taskId,omitempty"`
	IDs             RemoteIDs       `json:"ids"`
	CreateAttempted bool            `json:"createAttempted,omitempty"`
	Ambiguous       bool            `json:"ambiguous,omitempty"`
	Checks          int             `json:"checks,omitempty"`
}
type Result struct {
	Progress Progress
	Status   string
	Message  string
}
type Publisher interface {
	Advance(ctx context.Context, channel string, request Request, progress Progress) (Result, error)
}

// ChannelPublishers separates capabilities while sharing durable receipt and
// lease handling. A missing channel can never fall through to another vendor.
type ChannelPublishers map[string]Publisher

func (p ChannelPublishers) Advance(ctx context.Context, channel string, request Request, progress Progress) (Result, error) {
	provider := p[channel]
	if provider == nil {
		return Result{}, fmt.Errorf("публикация %s не подключена", channel)
	}
	return provider.Advance(ctx, channel, request, progress)
}

func SiteAttributeCode(a Attribute) string {
	n := strings.ToLower(a.Name)
	if strings.Contains(n, "диаметр") && (strings.Contains(n, "горш") || strings.Contains(n, "кашпо")) {
		return "pot_diameter_cm"
	}
	return ""
}

type RemoteCard struct {
	Card        Request   `json:"card"`
	ExternalIDs RemoteIDs `json:"externalIds"`
}
