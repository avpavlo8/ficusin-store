package ordersale

import (
	"errors"
	"github.com/avpavlo8/ficusin-store/backend/internal/money"
	"math/big"
	"sort"
)

// netRevisionLines allocates an order-level discount in exact kopecks. A line
// splits into at most two unit-price groups, retaining its full quantity.
func netRevisionLines(lines []Line, net int64) ([]Line, error) {
	type allocation struct {
		index     int
		minor     int64
		remainder *big.Int
	}
	var gross int64
	alloc := make([]allocation, len(lines))
	amounts := make([]int64, len(lines))
	for i, l := range lines {
		price, e := money.Parse(l.UnitPrice)
		if e != nil || price < 0 || l.Quantity <= 0 || int64(l.Quantity) > money.MaxMinor/max(price, 1) {
			return nil, errors.New("invalid revision amount")
		}
		amounts[i] = price * int64(l.Quantity)
		if gross > money.MaxMinor-amounts[i] {
			return nil, errors.New("revision amount overflow")
		}
		gross += amounts[i]
	}
	if net < 0 || net > gross {
		return nil, errors.New("скидка или сумма заказа не совпадает с позициями")
	}
	if gross == 0 {
		if net != 0 {
			return nil, ErrInput
		}
		return append([]Line(nil), lines...), nil
	}
	var used int64
	for i, amount := range amounts {
		n := new(big.Int).Mul(big.NewInt(net), big.NewInt(amount))
		q, r := new(big.Int), new(big.Int)
		q.QuoRem(n, big.NewInt(gross), r)
		alloc[i] = allocation{i, q.Int64(), r}
		used += q.Int64()
	}
	sort.SliceStable(alloc, func(i, j int) bool {
		c := alloc[i].remainder.Cmp(alloc[j].remainder)
		if c != 0 {
			return c > 0
		}
		return lines[alloc[i].index].OrderItemID < lines[alloc[j].index].OrderItemID
	})
	for i := int64(0); i < net-used; i++ {
		alloc[i].minor++
	}
	sort.Slice(alloc, func(i, j int) bool { return alloc[i].index < alloc[j].index })
	result := []Line{}
	for _, a := range alloc {
		l := lines[a.index]
		q := int64(l.Quantity)
		base, extra := a.minor/q, a.minor%q
		if q-extra > 0 {
			low := l
			low.Quantity = int(q - extra)
			low.UnitPrice = money.Format(base)
			result = append(result, low)
		}
		if extra > 0 {
			high := l
			high.Quantity = int(extra)
			high.UnitPrice = money.Format(base + 1)
			result = append(result, high)
		}
	}
	return result, nil
}
