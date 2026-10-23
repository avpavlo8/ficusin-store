package procurement

import (
	"github.com/avpavlo8/ficusin-store/backend/internal/money"
	"math/big"
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
	cents := money.Kopecks(total)
	type share struct {
		item     allocationWeight
		cents    int64
		fraction *big.Rat
	}
	eligible := []share{}
	weightTotal := new(big.Rat)
	for _, item := range weights {
		if item.Weight > 0 && finiteNonnegative(item.Weight) && item.Quantity > 0 {
			eligible = append(eligible, share{item: item})
			weightTotal.Add(weightTotal, money.Decimal(item.Weight))
		}
	}
	if cents <= 0 || weightTotal.Sign() <= 0 {
		return result
	}
	remaining := cents
	for i := range eligible {
		exact := new(big.Rat).Mul(new(big.Rat).SetInt64(cents), money.Decimal(eligible[i].item.Weight))
		exact.Quo(exact, weightTotal)
		whole := new(big.Int).Quo(exact.Num(), exact.Denom()).Int64()
		if whole > remaining {
			whole = remaining
		}
		eligible[i].cents = whole
		eligible[i].fraction = new(big.Rat).Sub(exact, new(big.Rat).SetInt64(whole))
		remaining -= whole
	}
	sort.SliceStable(eligible, func(i, j int) bool { return eligible[i].fraction.Cmp(eligible[j].fraction) > 0 })
	for i := int64(0); i < remaining; i++ {
		eligible[i%int64(len(eligible))].cents++
	}
	for _, v := range eligible {
		result[v.item.ID] = decimalFloat(big.NewRat(v.cents, 100*int64(v.item.Quantity)))
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
	purchaseDecimal := money.Decimal(unitPrice)
	if kind == KindInternational {
		purchaseDecimal.Mul(purchaseDecimal, money.Decimal(exchangeRate))
	}
	extra := 0.
	if len(additional) > 0 {
		extra = additional[0]
	}
	purchase, _ := purchaseDecimal.Float64()
	unitDecimal := new(big.Rat).Add(purchaseDecimal, money.Decimal(trolleyPerUnit))
	unitDecimal.Add(unitDecimal, money.Decimal(ryazanPerUnit))
	unitDecimal.Add(unitDecimal, money.Decimal(extra))
	unitCost, _ := unitDecimal.Float64()
	retailBase := new(big.Rat).Mul(unitDecimal, money.Decimal(settings.RetailMarkupMultiplier))
	retail := roundRetailDecimal(retailBase, settings.RoundPrices)
	if settings.Economics.Unified != nil && settings.Economics.Unified.Enabled {
		if q := unifiedChannelQuotesDecimal(settings, unitCost, nil, unitDecimal)["site"]; q.PriceRUB != nil {
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
	if !finiteNonnegative(value) || value > 1e12 {
		return 0
	}
	return roundRetailDecimal(money.Decimal(value), enabled)
}
func roundRetailDecimal(value *big.Rat, enabled bool) int64 {
	if value.Sign() <= 0 || value.Cmp(big.NewRat(1_000_000_000_000, 1)) > 0 {
		return 0
	}
	if !enabled {
		return money.Round(value)
	}
	hundred := new(big.Int).Quo(value.Num(), new(big.Int).Mul(value.Denom(), big.NewInt(100))).Int64() * 100
	best := int64(0)
	var distance *big.Rat
	for _, candidate := range []int64{hundred - 50, hundred - 10, hundred + 50, hundred + 90, hundred + 150} {
		if candidate <= 0 {
			continue
		}
		delta := new(big.Rat).Sub(value, new(big.Rat).SetInt64(candidate))
		delta.Abs(delta)
		if distance == nil || delta.Cmp(distance) < 0 || delta.Cmp(distance) == 0 && candidate > best {
			best = candidate
			distance = delta
		}
	}
	return best
}

func deliveryPerTrolley(total float64, trolleyCount int) float64 {
	if total <= 0 || trolleyCount <= 0 {
		return 0
	}
	return decimalFloat(new(big.Rat).Quo(money.Decimal(total), new(big.Rat).SetInt64(int64(trolleyCount))))
}

func priceChangeNeeded(current float64, proposed int64, threshold float64) bool {
	if proposed <= 0 {
		return false
	}
	if current <= 0 {
		return true
	}
	difference := new(big.Rat).Sub(new(big.Rat).SetInt64(proposed), money.Decimal(current))
	difference.Abs(difference)
	return difference.Cmp(new(big.Rat).Mul(money.Decimal(current), money.Decimal(threshold))) > 0
}
