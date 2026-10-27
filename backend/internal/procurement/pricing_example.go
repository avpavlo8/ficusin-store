package procurement

import (
	"context"
	"math/big"
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
	Mode       string          `json:"mode,omitempty"`
	MarkupRate *float64        `json:"markupRate,omitempty"`
	CostRUB    float64         `json:"costRub"`
	RetailRUB  float64         `json:"retailRub"`
	Package    *PricingPackage `json:"package,omitempty"`
}

type UnitPricePreview struct {
	RetailRUB float64                 `json:"retailRub"`
	Quotes    map[string]ChannelQuote `json:"quotes"`
}

func (service *Service) PreviewUnitPrice(ctx context.Context, input UnitPriceInput) (UnitPricePreview, error) {
	if err := validateUnitPriceInput(input); err != nil {
		return UnitPricePreview{}, err
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
	return previewUnitPrice(settings, input)
}

func validateUnitPriceInput(input UnitPriceInput) error {
	if input.Mode != "" && input.Mode != "cost" && input.Mode != "retail" {
		return ErrInvalidInput
	}
	if !finiteNonnegative(input.CostRUB) || input.CostRUB > 1e9 || !finiteNonnegative(input.RetailRUB) || input.RetailRUB > 1e9 {
		return ErrInvalidInput
	}
	if input.Mode == "retail" {
		if input.RetailRUB <= 0 {
			return ErrInvalidInput
		}
	} else if input.CostRUB <= 0 || (input.Mode == "" && input.RetailRUB <= 0) {
		return ErrInvalidInput
	}
	if input.MarkupRate != nil && (!finiteNonnegative(*input.MarkupRate) || *input.MarkupRate > 10) {
		return ErrInvalidInput
	}
	if input.Package != nil && !input.Package.valid() {
		return ErrInvalidInput
	}
	return nil
}

// Pure preview: never persists prices or modifies the shared pricing settings.
func previewUnitPrice(settings PricingSettings, input UnitPriceInput) (UnitPricePreview, error) {
	if err := validateUnitPriceInput(input); err != nil {
		return UnitPricePreview{}, err
	}
	unified := settings.Economics.Unified != nil && settings.Economics.Unified.Enabled
	if input.Mode == "retail" {
		// Cost cancels when equating the net receipts of site and marketplace sales.
		// Use the shared parcel tariffs, without estimating an unknown purchase cost.
		quotes := map[string]ChannelQuote{}
		if unified {
			quotes = unifiedChannelQuotesTarget(settings, 1, input.Package, 0)
		}
		site := settings.Economics.Channels["site"]
		if len(site.missing()) != 0 || !validateChannelCosts(site, settings.TaxRate) {
			return UnitPricePreview{}, ErrInvalidInput
		}
		net := contributionDecimal(input.RetailRUB, 0, settings.TaxRate, site)
		for _, ch := range []string{"wb", "ozon"} {
			q := quotes[ch]
			if unified && len(q.Missing) != 0 {
				continue
			}
			costs := settings.Economics.Channels[ch]
			if unified {
				costs = *q.Costs
			}
			q = ChannelQuote{Costs: &costs, Package: input.Package, Notes: q.Notes, Missing: costs.missing()}
			if !validateChannelCosts(costs, settings.TaxRate) {
				q.Missing = append(q.Missing, "проверьте расходы продажи")
			}
			if len(q.Missing) == 0 {
				priceExact := new(big.Rat).Quo(new(big.Rat).Add(net, decimalSum(*costs.PackagingRUB, *costs.LogisticsRUB, *costs.LossRUB)), retainedDecimal(settings.TaxRate, costs))
				price := decimalFloat(priceExact)
				if price <= 0 || price > 1e12 {
					q.Missing = []string{"проверьте цену магазина и расходы продажи"}
				} else {
					p := decimalCeil(priceExact)
					q.PriceRUB = &p
				}
			}
			quotes[ch] = q
		}
		delete(quotes, "site")
		return UnitPricePreview{RetailRUB: input.RetailRUB, Quotes: quotes}, nil
	}
	markup := settings.RetailMarkupMultiplier - 1
	if unified {
		markup = settings.Economics.Unified.MarkupRate
	}
	if input.MarkupRate != nil {
		markup = *input.MarkupRate
	}
	retailInput := input.RetailRUB
	if input.Mode == "cost" {
		retailInput = decimalProduct(input.CostRUB, decimalFloat(decimalSum(1, markup)))
	}
	retail := roundRetail(retailInput, settings.RoundPrices)
	if unified {
		quotes := unifiedChannelQuotesTarget(settings, input.CostRUB, input.Package, markup)
		if quotes["site"].PriceRUB != nil {
			retail = *quotes["site"].PriceRUB
		}
		return UnitPricePreview{RetailRUB: float64(retail), Quotes: quotes}, nil
	}
	return UnitPricePreview{RetailRUB: float64(retail), Quotes: calculateChannelQuotes(settings, input.CostRUB, retail, nil)}, nil
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
