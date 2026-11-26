package integration

import (
	"context"
	"math"
	"strconv"
	"strings"

	ce "github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
)

func procurementMarketplaceMeasurement(attribute ce.Attribute, raw any, channel string) any {
	if raw == nil || !attribute.Numeric(channel) || attribute.DictionaryID > 0 {
		return nil
	}
	values := ce.Values(raw)
	if len(values) != 1 {
		return nil
	}
	value, err := strconv.ParseFloat(values[0].Value, 64)
	if err != nil || value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return nil
	}
	name := strings.ToLower(attribute.Name)
	if strings.Contains(name, "мм") {
		return value * 10
	}
	return value
}

func hasProcurementMarketplaceMeasurement(r ce.Request, channel string) bool {
	for _, attribute := range r.Schema {
		code := ce.SiteAttributeCode(attribute)
		if code != "" && procurementMarketplaceMeasurement(attribute, r.SiteAttributes[code], channel) != nil {
			return true
		}
	}
	return false
}

func (p *CatalogPublisher) verifyProcurementMarketplaceMeasurements(ctx context.Context, channel string, r ce.Request, state ce.Progress) (ce.Result, error) {
	current, ids, err := p.hydrateCatalogUpdate(ctx, channel, ce.Request{ExistingID: r.ExistingID})
	if err != nil {
		return ce.Result{Progress: state}, err
	}
	if ids.ProductID != state.IDs.ProductID || ids.OfferID != state.IDs.OfferID {
		return ce.Result{Progress: state, Status: "needs_attention", Message: "Связь карточки изменилась во время обновления размеров."}, nil
	}
	verified := map[string]bool{}
	for _, attribute := range current.Schema {
		code := ce.SiteAttributeCode(attribute)
		desired, ok := procurementMarketplaceMeasurement(attribute, r.SiteAttributes[code], channel).(float64)
		if !ok || code == "" {
			continue
		}
		values := ce.Values(current.Channel.Attributes[strconv.FormatInt(attribute.Key(), 10)])
		if len(values) != 1 {
			continue
		}
		actual, err := strconv.ParseFloat(strings.ReplaceAll(values[0].Value, ",", "."), 64)
		if err == nil && math.Abs(actual-desired) < 0.00001 {
			verified[code] = true
		}
	}
	if len(verified) == len(r.SiteAttributes) {
		state.Ambiguous = false
		state.Stage = "done"
		return ce.Result{Progress: state, Status: "synced", Message: "Площадка подтвердила размеры растения из закупки."}, nil
	}
	state.Checks++
	if state.Checks >= 15 {
		return ce.Result{Progress: state, Status: "needs_attention", Message: "Площадка не подтвердила все размеры растения. Проверьте доступные характеристики и карточку."}, nil
	}
	return ce.Result{Progress: state, Status: "queued", Message: "Ожидаем подтверждения размеров растения на площадке."}, nil
}
