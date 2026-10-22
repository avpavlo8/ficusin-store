package admin

import (
	"math"
	"testing"
)

func TestOrderDiscountUsesMinorUnits(t *testing.T) {
	for _, tc := range []struct {
		kind        string
		value       float64
		gross, want int64
		invalid     bool
	}{
		{"fixed", 69, 69000, 6900, false}, {"percent", 10, 69000, 6900, false}, {"percent", 12.5, 10001, 1250, false}, {"fixed", 0, 69000, 0, false}, {"percent", 100, 69000, 69000, false}, {"percent", 101, 69000, 0, true}, {"fixed", 691, 69000, 0, true}, {"fixed", -1, 69000, 0, true}, {"fixed", math.NaN(), 69000, 0, true}, {"fixed", math.Inf(1), 69000, 0, true},
	} {
		got, err := discountMinor(tc.gross, OrderDiscount{Type: tc.kind, Value: tc.value})
		if (err != nil) != tc.invalid || !tc.invalid && got != tc.want {
			t.Fatalf("%+v => %d %v", tc, got, err)
		}
	}
}
