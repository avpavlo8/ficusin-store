package procurement

import (
	"math"
	"sort"
)

type calculatedLine struct {
	AdditionalPurchaseUnitRUB    float64
	PurchaseUnitRUB              float64
	TrolleyDeliveryUnitRUB       float64
	RyazanDeliveryUnitRUB        float64
	UnitCostRUB                  float64
	ProposedRetailRUB            int64
	ProposedMarketplaceRUB       int64
	ProposedMarketplaceStrikeRUB int64
}

type allocationWeight struct {
	ID       int64
	Weight   float64
	Quantity int
}

// allocateMoneyPerUnit distributes kopecks by largest remainder. Independent
// rounding can over-allocate tiny totals and give the final row negative cost.
// Floors plus nonnegative remainders preserve the exact expense for any order.
func allocateMoneyPerUnit(total float64, weights []allocationWeight) map[int64]float64 {
	result := make(map[int64]float64, len(weights))
	if !finiteNonnegative(total) || total > 1e12 {
		return result
	}
	cents := int64(math.Round(total * 100))
	type share struct {
		item     allocationWeight
		cents    int64
		fraction float64
	}
	eligible := []share{}
	weightTotal := 0.
	for _, item := range weights {
		if item.Weight > 0 && finiteNonnegative(item.Weight) && item.Quantity > 0 {
			eligible = append(eligible, share{item: item})
			weightTotal += item.Weight
		}
	}
	if cents <= 0 || weightTotal <= 0 || math.IsInf(weightTotal, 0) {
		return result
	}
	remaining := cents
	for i := range eligible {
		exact := float64(cents) * eligible[i].item.Weight / weightTotal
		whole := int64(math.Floor(exact))
		if whole > remaining {
			whole = remaining
		}
		eligible[i].cents = whole
		eligible[i].fraction = exact - float64(whole)
		remaining -= whole
	}
	sort.SliceStable(eligible, func(i, j int) bool { return eligible[i].fraction > eligible[j].fraction })
	for i := int64(0); i < remaining; i++ {
		eligible[i%int64(len(eligible))].cents++
	}
	for _, v := range eligible {
		result[v.item.ID] = float64(v.cents) / 100 / float64(v.item.Quantity)
	}
	return result
}

// calculateAllocatedLine applies the pricing formula after logistics has been
// reconciled at order level. This keeps every trolley and the Ryazan delivery
// exact instead of letting each row independently approximate its share.
func calculateAllocatedLine(
	settings PricingSettings,
	kind string,
	unitPrice, exchangeRate, trolleyPerUnit, ryazanPerUnit, heightCM float64, additional ...float64,
) calculatedLine {
	purchase := unitPrice
	if kind == KindInternational {
		purchase *= exchangeRate
	}
	extra := 0.
	if len(additional) > 0 {
		extra = additional[0]
	}
	unitCost := purchase + trolleyPerUnit + ryazanPerUnit + extra
	retailBase := unitCost * settings.RetailMarkupMultiplier
	retail := roundRetail(retailBase, settings.RoundPrices)
	if settings.Economics.Unified != nil && settings.Economics.Unified.Enabled {
		if q := UnifiedChannelQuotes(settings, unitCost, nil)["site"]; q.PriceRUB != nil {
			retail = *q.PriceRUB
		}
	}
	return calculatedLine{
		AdditionalPurchaseUnitRUB: extra, PurchaseUnitRUB: purchase, TrolleyDeliveryUnitRUB: trolleyPerUnit,
		RyazanDeliveryUnitRUB: ryazanPerUnit, UnitCostRUB: unitCost,
		ProposedRetailRUB: retail,
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
