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

// Use the production pricing calculation for both sides; this endpoint only reads.
func (service *Service) PricingExample(ctx context.Context, input PricingSettings) (PricingExample, error) {
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
