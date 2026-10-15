package procurement

import (
	"context"
	"math"
)

// PlanPreview never authorizes receipts or marketplace price writes.
type PlanPreview struct {
	Lines []PlanPreviewLine `json:"lines"`
}

type PlanPreviewLine struct {
	Purchase      float64                 `json:"purchase"`
	Cost          float64                 `json:"cost"`
	Retail        int64                   `json:"retail"`
	ChannelQuotes map[string]ChannelQuote `json:"channelQuotes"`
}

// PreviewPlan uses the same rounding and pricing implementation as PDF invoices.
func (service *Service) PreviewPlan(ctx context.Context, input PlanCreate) (PlanPreview, error) {
	previewer, ok := service.store.(interface {
		PreviewPlan(context.Context, PlanCreate) (PlanPreview, error)
	})
	if !ok {
		return PlanPreview{}, ErrInvalidInput
	}
	return previewer.PreviewPlan(ctx, input)
}

func (store *PostgresStore) PreviewPlan(ctx context.Context, input PlanCreate) (PlanPreview, error) {
	settings, err := store.loadSettings(ctx)
	if err != nil {
		return PlanPreview{}, err
	}
	var kind string
	if err := store.pool.QueryRow(ctx, `SELECT kind FROM procurement_suppliers WHERE id=$1 AND active`, input.SupplierID).Scan(&kind); err != nil {
		return PlanPreview{}, ErrNotFound
	}
	_, preview, err := calculatePlan(settings, kind, input)
	return preview, err
}

func calculatePlan(settings PricingSettings, kind string, input PlanCreate) ([]calculatedLine, PlanPreview, error) {
	if input.Costs == nil || len(input.Items) == 0 || len(input.Items) > 500 {
		return nil, PlanPreview{}, ErrInvalidInput
	}
	costs := *input.Costs
	for _, value := range []float64{costs.ExchangeRate, costs.DeliveryToMoscowRUB, costs.DeliveryToRyazanRUB, costs.AdditionalCostsRUB} {
		if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, PlanPreview{}, ErrInvalidInput
		}
	}
	if costs.ExchangeRate <= 0 {
		return nil, PlanPreview{}, ErrInvalidInput
	}
	volumes := make(map[string]float64)
	totalHeight := 0.0
	for _, item := range input.Items {
		if item.Quantity <= 0 || item.Quantity > 1000000 || item.ExpectedUnitPrice <= 0 || math.IsNaN(item.ExpectedUnitPrice) || math.IsInf(item.ExpectedUnitPrice, 0) {
			return nil, PlanPreview{}, ErrInvalidInput
		}
		if kind == KindInternational && (item.HeightCM == nil || *item.HeightCM <= 0 || math.IsNaN(*item.HeightCM) || math.IsInf(*item.HeightCM, 0)) {
			return nil, PlanPreview{}, ErrInvalidInput
		}
		if kind == KindInternational {
			if item.PotDiameterCM == nil || *item.PotDiameterCM <= 0 || math.IsNaN(*item.PotDiameterCM) || math.IsInf(*item.PotDiameterCM, 0) {
				return nil, PlanPreview{}, ErrInvalidInput
			}
			volumes[item.LoadUnit] += math.Pi * math.Pow(*item.PotDiameterCM/2, 2) * *item.HeightCM * float64(item.Quantity)
		}
		if item.HeightCM != nil {
			totalHeight += *item.HeightCM * float64(item.Quantity)
		}
	}
	if math.IsInf(totalHeight, 0) {
		return nil, PlanPreview{}, ErrInvalidInput
	}
	lines := make([]calculatedLine, 0, len(input.Items))
	preview := PlanPreview{Lines: make([]PlanPreviewLine, 0, len(input.Items))}

	moscowWeights, ryazanWeights, extraWeights := []allocationWeight{}, []allocationWeight{}, []allocationWeight{}
	for i, item := range input.Items {
		value := item.ExpectedUnitPrice * float64(item.Quantity)
		mw, rw := value, value
		if kind == KindInternational {
			volume := volumes[item.LoadUnit]
			if volume <= 0 || math.IsInf(volume, 0) {
				return nil, PlanPreview{}, ErrInvalidInput
			}
			mw = math.Pi * math.Pow(*item.PotDiameterCM/2, 2) * *item.HeightCM * float64(item.Quantity) / volume / float64(len(volumes))
			rw = *item.HeightCM * float64(item.Quantity)
		}
		id := int64(i)
		moscowWeights = append(moscowWeights, allocationWeight{id, mw, item.Quantity})
		ryazanWeights = append(ryazanWeights, allocationWeight{id, rw, item.Quantity})
		extraWeights = append(extraWeights, allocationWeight{id, value, item.Quantity})
	}
	moscow := allocateMoneyPerUnit(costs.DeliveryToMoscowRUB, moscowWeights)
	ryazan := allocateMoneyPerUnit(costs.DeliveryToRyazanRUB, ryazanWeights)
	extra := allocateMoneyPerUnit(costs.AdditionalCostsRUB, extraWeights)
	for i, item := range input.Items {
		height := 0.
		if item.HeightCM != nil {
			height = *item.HeightCM
		}
		line := calculateAllocatedLine(settings, kind, item.ExpectedUnitPrice, costs.ExchangeRate, moscow[int64(i)], ryazan[int64(i)], height, extra[int64(i)])
		lines = append(lines, line)
		preview.Lines = append(preview.Lines, PlanPreviewLine{Purchase: line.PurchaseUnitRUB, Cost: line.UnitCostRUB, Retail: line.ProposedRetailRUB, ChannelQuotes: calculateChannelQuotes(settings, line.UnitCostRUB, line.ProposedRetailRUB, nil)})
	}
	return lines, preview, nil
}
