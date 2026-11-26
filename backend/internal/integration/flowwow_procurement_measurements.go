package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	ce "github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
	"github.com/avpavlo8/ficusin-store/backend/internal/money"
)

var errFlowwowPlantMeasurementsUnavailable = errors.New("Flowwow plant measurements are unavailable")

type flowwowMeasurementReceipt struct {
	Properties map[string]any    `json:"properties"`
	Fields     map[string]string `json:"fields"`
}

func flowwowPlantMeasurement(field FlowwowField, attrs map[string]any) (string, any) {
	code := ce.SiteAttributeCode(ce.Attribute{Name: field.Name})
	if code == "" && field.Field == "height" && strings.EqualFold(strings.TrimSpace(field.Name), "Высота") {
		// The confirmed card is a plant, and only the product height field of
		// its own category is eligible. Packaging sizes have separate fields.
		code = "height_cm"
	}
	values := ce.Values(attrs[code])
	if code == "" || len(values) != 1 {
		return "", nil
	}
	value, err := strconv.ParseFloat(values[0].Value, 64)
	if err != nil || value <= 0 {
		return "", nil
	}
	if field.Type == "measure" {
		for _, unit := range field.Measure {
			switch strings.ToLower(strings.TrimSpace(unit.Name)) {
			case "см", "см.", "cm", "сантиметр", "сантиметры":
				return code, map[string]any{"val": value, "measure": unit.ID}
			}
		}
	}
	return "", nil
}

func (c *FlowwowClient) flowwowMeasurementUpdate(ctx context.Context, request ce.Request, offer string, card *FlowwowProduct) (map[string]any, json.RawMessage, error) {
	if card == nil || len(card.Images) > 6 {
		return nil, nil, errors.New("Flowwow: проверьте связанную карточку и её текущий набор фотографий")
	}
	price, err := strconv.ParseFloat(card.Price, 64)
	if err != nil {
		return nil, nil, err
	}
	request.PriceMinor, err = money.Minor(price)
	if err != nil {
		return nil, nil, err
	}
	request.Name, request.Description, request.Images = card.Name, card.Description, card.Images
	request.Channel.Category, request.Channel.Attributes = ce.Category{}, nil
	fields := map[string]string{}
	item, err := c.productPayloadWithMeasurements(ctx, request, offer, card, fields)
	if err != nil {
		return nil, nil, err
	}
	properties := map[string]any{}
	for field := range fields {
		properties[field] = item[field]
	}
	encoded, err := json.Marshal(flowwowMeasurementReceipt{Properties: properties, Fields: fields})
	return item, encoded, err
}

func flowwowMeasurementReadback(request ce.Request, state ce.Progress, card *FlowwowProduct) ce.Result {
	var receipt flowwowMeasurementReceipt
	verified := map[string]bool{}
	if card != nil && strconv.FormatInt(card.ProductID, 10) == state.IDs.ProductID && json.Unmarshal(state.Native, &receipt) == nil {
		for field, want := range receipt.Properties {
			desired, _ := json.Marshal(want)
			actual, _ := json.Marshal(card.Properties[field])
			if bytes.Equal(actual, desired) {
				verified[receipt.Fields[field]] = true
			}
		}
	}
	if len(request.SiteAttributes) > 0 && len(verified) == len(request.SiteAttributes) {
		state.Ambiguous = false
		state.Stage = "done"
		return ce.Result{Progress: state, Status: "synced", Message: "Flowwow подтвердил размеры растения из закупки."}
	}
	if state.Checks >= 15 {
		return ce.Result{Progress: state, Status: "needs_attention", Message: "Flowwow не подтвердил все размеры растения. Проверьте доступные характеристики и карточку."}
	}
	return ce.Result{Progress: state, Status: "queued", Message: "Ожидаем подтверждения размеров растения в Flowwow."}
}
