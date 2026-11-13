package admin

import (
	"context"
	"encoding/json"
	"fmt"
	ce "github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
	"github.com/jackc/pgx/v5"
	"math"
	"strconv"
	"strings"
)

func editorSchema(ctx context.Context, tx pgx.Tx, channel string, ch EditorChannel, mode string) ([]ce.Attribute, error) {
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT payload FROM catalog_editor_references WHERE channel=$1 AND kind=$3 AND scope=$2 AND fetched_at>NOW()-INTERVAL '24 hours'`, channel, fmt.Sprintf("%s:%d:0:", ch.Category.ID, ch.Category.TypeID), editorReferenceKind(mode, "attributes")).Scan(&raw)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("%w: сначала загрузите характеристики категории %s", ErrInvalidInput, channel)
	}
	if err != nil {
		return nil, err
	}
	var schema struct {
		Result []ce.Attribute `json:"result"`
		Data   []ce.Attribute `json:"data"`
	}
	if err = json.Unmarshal(raw, &schema); err != nil {
		return nil, err
	}
	return append(schema.Result, schema.Data...), nil
}
func validateEditorDictionaries(ctx context.Context, tx pgx.Tx, v EditorVariantPlan, mode string, siteAttributes map[string]any) error {
	for channel, ch := range map[string]EditorChannel{"ozon": v.Marketplace.Ozon, "wb": v.Marketplace.WB} {
		if !ch.Enabled || ch.Mode == "link" {
			continue
		}
		price := v.OzonPrice
		if channel == "wb" {
			price = v.WBPrice
		}
		n, e := strconv.ParseFloat(strings.ReplaceAll(price, ",", "."), 64)
		if e != nil || math.IsNaN(n) || math.IsInf(n, 0) || n <= 0 || n > 1e12 {
			return fmt.Errorf("%w: дождитесь расчёта цены %s или задайте её вручную", ErrInvalidInput, channel)
		}
		var dimensions bool
		if e = tx.QueryRow(ctx, `SELECT COALESCE(shipping_package_dimension($1,'package_length_cm'),0)>0 AND COALESCE(shipping_package_dimension($1,'package_width_cm'),0)>0 AND COALESCE(shipping_package_dimension($1,'package_height_cm'),0)>0 AND COALESCE(variant_numeric_attribute($1,'package_weight_grams'),0)>0`, v.VariantID).Scan(&dimensions); e != nil {
			return e
		}
		if !dimensions {
			return fmt.Errorf("%w: заполните вес и размеры выбранного сезона упаковки", ErrInvalidInput)
		}
		schema, e := editorSchema(ctx, tx, channel, ch, mode)
		if e != nil {
			return e
		}
		for _, a := range schema {
			if a.Role(channel) == "vat" {
				continue
			}
			raw := ch.Attributes[strconv.FormatInt(a.Key(), 10)]
			if code := ce.SiteAttributeCode(a); code != "" {
				raw = siteAttributes[code]
			}
			if a.Role(channel) != "" {
				raw = ce.CommonValue(a, v.Marketplace, channel, "Название из карточки", "Описание из карточки")
			}
			values, e := ce.AttributeValues(a, raw, channel)
			if e != nil {
				return fmt.Errorf("%w: %s: %v", ErrInvalidInput, channel, e)
			}
			if channel == "ozon" && a.DictionaryID > 0 && a.Role(channel) != "type" {
				for _, value := range values {
					var exists bool
					// Values must have actually been offered for this category/type/attribute.
					if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM catalog_editor_references r CROSS JOIN LATERAL jsonb_array_elements(COALESCE(r.payload->'result','[]')) item WHERE channel='ozon' AND kind=$4 AND scope LIKE $1 AND fetched_at>NOW()-INTERVAL '24 hours' AND item->>'id'=$2 AND item->>'value'=$3)`, fmt.Sprintf("%s:%d:%d:%%", ch.Category.ID, ch.Category.TypeID, a.ID), strconv.FormatInt(value.DictionaryID, 10), value.Value, editorReferenceKind(mode, "values")).Scan(&exists); e != nil {
						return e
					}
					if !exists {
						return fmt.Errorf("%w: %s: заново выберите «%s» из справочника", ErrInvalidInput, channel, a.Name)
					}
				}
			}
		}
	}
	return nil
}
