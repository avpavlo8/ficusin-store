package ordersale

import (
	"github.com/avpavlo8/ficusin-store/backend/internal/money"
	"testing"
)

func TestNetRevisionLinesPreservesKopecksAndQuantities(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lines []Line
		net   int64
		bad   bool
	}{
		{"three units one ruble discount", []Line{{OrderItemID: 1, VariantID: 1, Quantity: 3, UnitPrice: "100.00"}}, 29900, false},
		{"unequal prices and quantities", []Line{{OrderItemID: 1, VariantID: 1, Quantity: 7, UnitPrice: "12.34"}, {OrderItemID: 2, VariantID: 2, Quantity: 3, UnitPrice: "99.99"}}, 30001, false},
		{"zero priced gift", []Line{{OrderItemID: 1, VariantID: 1, Quantity: 3, UnitPrice: "0.00"}, {OrderItemID: 2, VariantID: 2, Quantity: 1, UnitPrice: "1.00"}}, 99, false},
		{"zero total", []Line{{OrderItemID: 1, VariantID: 1, Quantity: 3, UnitPrice: "0.00"}}, 0, false},
		{"full discount", []Line{{OrderItemID: 1, VariantID: 1, Quantity: 3, UnitPrice: "1.00"}}, 0, false},
		{"over discount", []Line{{Quantity: 1, UnitPrice: "1.00"}}, -1, true},
		{"net above gross", []Line{{Quantity: 1, UnitPrice: "1.00"}}, 101, true},
		{"overflow", []Line{{Quantity: 3, UnitPrice: "999999999999.99"}}, 100, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := netRevisionLines(tc.lines, tc.net)
			if (err != nil) != tc.bad {
				t.Fatal(out, err)
			}
			if tc.bad {
				return
			}
			var sum int64
			qty := map[int64]int{}
			for _, l := range out {
				p, e := money.Parse(l.UnitPrice)
				if e != nil {
					t.Fatal(e)
				}
				sum += p * int64(l.Quantity)
				qty[l.OrderItemID] += l.Quantity
			}
			if sum != tc.net {
				t.Fatalf("sum=%d net=%d", sum, tc.net)
			}
			for _, l := range tc.lines {
				if qty[l.OrderItemID] != l.Quantity {
					t.Fatal("quantity changed")
				}
			}
			if tc.name == "three units one ruble discount" && (len(out) != 2 || out[0].UnitPrice != "99.66" || out[0].Quantity != 1 || out[1].UnitPrice != "99.67" || out[1].Quantity != 2) {
				t.Fatal("incorrect deterministic split", out)
			}
		})
	}
}
