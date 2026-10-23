package procurement

import (
	"github.com/avpavlo8/ficusin-store/backend/internal/money"
	"math"
	"math/big"
)

func decimalFloor(v *big.Rat) int64 {
	q, r := new(big.Int), new(big.Int)
	q.QuoRem(v.Num(), v.Denom(), r)
	if r.Sign() < 0 {
		q.Sub(q, big.NewInt(1))
	}
	return q.Int64()
}

// LegacyMarketplacePrices preserves the legacy additive-rate rule, including
// floor of marketplace price before computing the strike price. Unified mode
// still replaces these proposals with UnifiedVariantPriceQuotes.
func LegacyMarketplacePrices(retailMinor int64, packaging, height, logistics, returns, marketplace, tax, reserve, strikeRate float64) (int64, int64) {
	retail := big.NewRat(retailMinor, 100)
	shipping := new(big.Rat).Mul(money.Decimal(math.Max(height, 0)), money.Decimal(math.Max(logistics, 0)))
	base := new(big.Rat).Add(retail, decimalSum(packaging))
	base.Add(base, shipping)
	market := decimalFloor(new(big.Rat).Mul(base, decimalSum(1, returns, marketplace, tax, reserve)))
	strike := decimalFloor(new(big.Rat).Mul(new(big.Rat).SetInt64(market), decimalSum(1, strikeRate)))
	return market, strike
}
func CompareAtPrice(price int64, markup float64) int64 {
	return decimalCeil(new(big.Rat).Mul(new(big.Rat).SetInt64(price), decimalSum(1, markup)))
}
