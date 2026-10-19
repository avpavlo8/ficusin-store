package procurement

import (
	"context"
	"time"
)

type PricingVersion struct {
	Version int       `json:"version"`
	At      time.Time `json:"at"`
}
type PricingExample struct {
	Before  PlanPreviewLine  `json:"before"`
	After   PlanPreviewLine  `json:"after"`
	History []PricingVersion `json:"history"`
}

type UnitPriceInput struct {
	CostRUB   float64         `json:"costRub"`
	RetailRUB float64         `json:"retailRub"`
	Package   *PricingPackage `json:"package,omitempty"`
}

type UnitPricePreview struct {
	RetailRUB int64                   `json:"retailRub"`
	Quotes    map[string]ChannelQuote `json:"quotes"`
}

func (service *Service) PreviewUnitPrice(ctx context.Context, input UnitPriceInput) (UnitPricePreview, error) {
	if !finiteNonnegative(input.CostRUB) || !finiteNonnegative(input.RetailRUB) || input.CostRUB <= 0 || input.CostRUB > 1e9 || input.RetailRUB <= 0 || input.RetailRUB > 1e9 {
		return UnitPricePreview{}, ErrInvalidInput
	}
	store, ok := service.store.(interface {
		PreviewUnitPrice(context.Context, UnitPriceInput) (UnitPricePreview, error)
	})
	if !ok {
		return UnitPricePreview{}, ErrInvalidInput
	}
	return store.PreviewUnitPrice(ctx, input)
}

func (store *PostgresStore) PreviewUnitPrice(ctx context.Context, input UnitPriceInput) (UnitPricePreview, error) {
	settings, err := store.loadSettings(ctx)
	if err != nil {
		return UnitPricePreview{}, err
	}
	retail := roundRetail(input.RetailRUB, settings.RoundPrices)
	if settings.Economics.Unified != nil && settings.Economics.Unified.Enabled {
		quotes := UnifiedChannelQuotes(settings, input.CostRUB, input.Package)
		if quotes["site"].PriceRUB != nil {
			retail = *quotes["site"].PriceRUB
		}
		return UnitPricePreview{RetailRUB: retail, Quotes: quotes}, nil
	}
	return UnitPricePreview{RetailRUB: retail, Quotes: calculateChannelQuotes(settings, input.CostRUB, retail, nil)}, nil
}

// Use the production pricing calculation for both sides; this endpoint only reads.
func (service *Service) PricingExample(ctx context.Context, input PricingSettings) (PricingExample, error) {
	var err error
	input, err = validatePricingSettings(input)
	if err != nil {
		return PricingExample{}, err
	}
	store, ok := service.store.(interface {
		PricingExample(context.Context, PricingSettings) (PricingExample, error)
	})
	if !ok {
		return PricingExample{}, ErrInvalidInput
	}
	return store.PricingExample(ctx, input)
}
func (store *PostgresStore) PricingExample(ctx context.Context, input PricingSettings) (PricingExample, error) {
	current, err := store.loadSettings(ctx)
	if err != nil {
		return PricingExample{}, err
	}
	pot, height := 14.0, 40.0
	example := PlanCreate{Costs: &CalculationInput{ExchangeRate: 120, DeliveryToMoscowRUB: 1200}, Items: []PlanItem{{RawName: "Пример", Quantity: 12, ExpectedUnitPrice: 4.85, PotDiameterCM: &pot, HeightCM: &height, LoadUnit: "shelf"}}}
	_, before, err := calculatePlan(current, KindInternational, example)
	if err != nil {
		return PricingExample{}, err
	}
	_, after, err := calculatePlan(input, KindInternational, example)
	if err != nil {
		return PricingExample{}, err
	}
	versions := []PricingVersion{}
	rows, err := store.pool.Query(ctx, `SELECT COALESCE((after_data->>'version')::INTEGER,0),created_at FROM admin_audit_log WHERE entity_type='procurement_settings' AND action='procurement.settings.update' ORDER BY created_at DESC LIMIT 10`)
	if err != nil {
		return PricingExample{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var v PricingVersion
		if err := rows.Scan(&v.Version, &v.At); err != nil {
			return PricingExample{}, err
		}
		versions = append(versions, v)
	}
	return PricingExample{Before: before.Lines[0], After: after.Lines[0], History: versions}, rows.Err()
}
