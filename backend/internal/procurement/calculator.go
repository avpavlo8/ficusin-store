package procurement

import "math"

type calculatedLine struct {
	PurchaseUnitRUB              float64
	TrolleyDeliveryUnitRUB       float64
	RyazanDeliveryUnitRUB        float64
	UnitCostRUB                  float64
	ProposedRetailRUB            int64
	ProposedMarketplaceRUB       int64
	ProposedMarketplaceStrikeRUB int64
}

// calculateAllocatedLine applies the pricing formula after logistics has been
// reconciled at order level. This keeps every trolley and the Ryazan delivery
// exact instead of letting each row independently approximate its share.
func calculateAllocatedLine(settings PricingSettings, kind string, unitPrice, exchangeRate, trolleyPerUnit, ryazanPerUnit float64) calculatedLine {
	purchase := unitPrice
	if kind == KindInternational {
		purchase *= exchangeRate
	}
	unitCost := purchase + trolleyPerUnit + ryazanPerUnit
	retailBase := unitCost * settings.RetailMarkupMultiplier
	retail := roundRetail(retailBase, settings.RoundPrices)
	marketplaceBase := float64(retail) + settings.PackageRUB
	marketplaceRate := 1 + settings.ReturnLossRate + settings.MarketplaceCostRate + settings.TaxRate + settings.ReserveRate
	marketplace := int64(math.Floor(marketplaceBase * marketplaceRate))
	strike := int64(math.Floor(float64(marketplace) * (1 + settings.MarketplaceStrikeMarkup)))
	return calculatedLine{
		PurchaseUnitRUB: purchase, TrolleyDeliveryUnitRUB: trolleyPerUnit,
		RyazanDeliveryUnitRUB: ryazanPerUnit, UnitCostRUB: unitCost,
		ProposedRetailRUB: retail, ProposedMarketplaceRUB: marketplace,
		ProposedMarketplaceStrikeRUB: strike,
	}
}

func roundRetail(value float64, enabled bool) int64 {
	if value <= 0 {
		return 0
	}
	if !enabled {
		return int64(math.Round(value))
	}

	// Prices are rounded to the nearest positive value ending in 50 or 90.
	// If two candidates are equally close, prefer the higher one so rounding
	// never loses margin merely because a value sits exactly in the middle.
	hundred := int64(math.Floor(value/100)) * 100
	candidates := []int64{hundred - 50, hundred - 10, hundred + 50, hundred + 90, hundred + 150}
	best := int64(0)
	bestDistance := math.MaxFloat64
	for _, candidate := range candidates {
		if candidate <= 0 {
			continue
		}
		distance := math.Abs(value - float64(candidate))
		if distance < bestDistance || distance == bestDistance && candidate > best {
			best, bestDistance = candidate, distance
		}
	}
	return best
}

func deliveryPerTrolley(total float64, trolleyCount int) float64 {
	if total <= 0 || trolleyCount <= 0 {
		return 0
	}
	return total / float64(trolleyCount)
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
