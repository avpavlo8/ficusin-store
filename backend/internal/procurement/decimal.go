package procurement

import (
	"github.com/avpavlo8/ficusin-store/backend/internal/money"
	"math/big"
)

// Rates, exchange conversions and per-unit allocations are rational decimals:
// rounding those to kopecks before applying the pricing rule would change it.
func decimalSum(values ...float64) *big.Rat {
	r := new(big.Rat)
	for _, value := range values {
		r.Add(r, money.Decimal(value))
	}
	return r
}
func decimalFloat(value *big.Rat) float64 { result, _ := value.Float64(); return result }
func decimalCeil(value *big.Rat) int64 {
	q, r := new(big.Int), new(big.Int)
	q.QuoRem(value.Num(), value.Denom(), r)
	if r.Sign() > 0 {
		q.Add(q, big.NewInt(1))
	}
	return q.Int64()
}
func retainedDecimal(tax float64, c ChannelCosts) *big.Rat {
	other := 0.
	if c.OtherRate != nil {
		other = *c.OtherRate
	}
	return new(big.Rat).Sub(big.NewRat(1, 1), decimalSum(tax, *c.CommissionRate, *c.AcquiringRate, other))
}
func contributionDecimal(price, cost, tax float64, c ChannelCosts) *big.Rat {
	return new(big.Rat).Sub(new(big.Rat).Mul(money.Decimal(price), retainedDecimal(tax, c)), decimalSum(cost, *c.PackagingRUB, *c.LogisticsRUB, *c.LossRUB))
}

func decimalProduct(values ...float64) float64 {
	product := big.NewRat(1, 1)
	for _, v := range values {
		product.Mul(product, money.Decimal(v))
	}
	return decimalFloat(product)
}
