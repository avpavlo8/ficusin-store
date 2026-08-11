package procurement

import "math"

type calculationLine struct {
	Kind          string
	Quantity      int
	UnitPrice     float64
	PotDiameterCM float64
	HeightCM      float64
}

type calculatedLine struct {
	PurchaseUnitRUB              float64
	TrolleyDeliveryUnitRUB       float64
	RyazanDeliveryUnitRUB        float64
	UnitCostRUB                  float64
	ProposedRetailRUB            int64
	ProposedMarketplaceRUB       int64
	ProposedMarketplaceStrikeRUB int64
}

// calculateLine is the versioned replacement for the Excel formulas. The
// Ryazan leg is distributed over the whole delivery by height; when its total
// is zero the share is zero, rather than silently inventing a cost.
func calculateLine(settings PricingSettings, costs OrderCosts, line calculationLine, totalHeightUnits float64) calculatedLine {
	purchase := line.UnitPrice
	if line.Kind == KindInternational {
		purchase *= costs.ExchangeRate
	}

	trolleyDelivery := 0.0
	if line.Kind == KindInternational && line.PotDiameterCM > 0 && line.HeightCM > 0 {
		volume := math.Pi * math.Pow(line.PotDiameterCM/2, 2) * line.HeightCM
		usableTrolleyVolume := settings.TrolleyVolumeCM3 * settings.TrolleyFillRatio
		if usableTrolleyVolume > 0 {
			trolleyDelivery = costs.TrolleyCostCurrency * costs.ExchangeRate * volume / usableTrolleyVolume
		}
	}

	ryazanDelivery := 0.0
	if totalHeightUnits > 0 && line.HeightCM > 0 {
		ryazanDelivery = costs.DeliveryToRyazanRUB * line.HeightCM / totalHeightUnits
	}
	unitCost := purchase + trolleyDelivery + ryazanDelivery

	retailBase := purchase * settings.DomesticRetailMultiplier
	if line.Kind == KindInternational {
		retailBase = (purchase*settings.InternationalCostMultiplier + trolleyDelivery) * settings.InternationalRetailMultiplier
	}
	retail := roundRetail(retailBase, settings.RetailRoundStep, settings.AvoidRoundHundreds)
	marketplaceBase := float64(retail) + ryazanDelivery + settings.PackageRUB
	marketplaceRate := 1 + settings.ReturnLossRate + settings.MarketplaceCostRate + settings.TaxRate + settings.ReserveRate
	marketplace := int64(math.Floor(marketplaceBase * marketplaceRate))
	strike := int64(math.Floor(float64(marketplace) * (1 + settings.MarketplaceStrikeMarkup)))

	return calculatedLine{
		PurchaseUnitRUB: purchase, TrolleyDeliveryUnitRUB: trolleyDelivery,
		RyazanDeliveryUnitRUB: ryazanDelivery, UnitCostRUB: unitCost,
		ProposedRetailRUB: retail, ProposedMarketplaceRUB: marketplace,
		ProposedMarketplaceStrikeRUB: strike,
	}
}

func roundRetail(value float64, step int, avoidHundreds bool) int64 {
	if value <= 0 || step <= 0 {
		return 0
	}
	result := int64(math.Ceil(value/float64(step))) * int64(step)
	if avoidHundreds && result%100 == 0 {
		result -= 10
	}
	return result
}

func priceChangeNeeded(current float64, proposed int64, threshold float64) bool {
	if proposed <= 0 {
		return false
	}
	if current <= 0 {
		return true
	}
	return math.Abs(float64(proposed)-current) > current*threshold
}
