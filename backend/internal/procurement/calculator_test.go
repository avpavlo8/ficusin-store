package procurement

import (
	"math"
	"testing"
)

func TestPriceChangeThresholdAppliesBothDirections(t *testing.T) {
	if !priceChangeNeeded(1000, 800, .1) || !priceChangeNeeded(1000, 1200, .1) {
		t.Fatal("changes above threshold must be proposed in both directions")
	}
	if priceChangeNeeded(1000, 1050, .1) {
		t.Fatal("small change must be suppressed")
	}
}

func TestAllocatedLineUsesReconciledLogisticsWithoutCurrencyConversion(t *testing.T) {
	settings := PricingSettings{
		RetailMarkupMultiplier: 2,
	}
	result := calculateAllocatedLine(settings, KindInternational, 10, 100, 250, 50, 0)
	if result.PurchaseUnitRUB != 1000 || result.TrolleyDeliveryUnitRUB != 250 || result.RyazanDeliveryUnitRUB != 50 || result.UnitCostRUB != 1300 {
		t.Fatalf("allocated calculation = %#v", result)
	}
	if result.ProposedRetailRUB != 2600 {
		t.Fatalf("retail = %d, want 2600", result.ProposedRetailRUB)
	}
}

func TestAllocateMoneyPerUnitPreservesEveryKopeck(t *testing.T) {
	allocated := allocateMoneyPerUnit(100.01, []allocationWeight{
		{ID: 1, Weight: 1, Quantity: 3},
		{ID: 2, Weight: 2, Quantity: 7},
		{ID: 3, Weight: 4, Quantity: 11},
	})
	total := allocated[1]*3 + allocated[2]*7 + allocated[3]*11
	if math.Abs(total-100.01) > .000001 {
		t.Fatalf("allocated total %.8f, want 100.01", total)
	}
}

func TestRoundRetailUsesNearestPsychologicalPrice(t *testing.T) {
	tests := []struct {
		value float64
		want  int64
	}{
		{440, 450},
		{980, 990},
		{451, 450},
		{100, 90},
		{120, 150}, // equal distance: prefer the higher price
		{1490, 1490},
	}
	for _, test := range tests {
		if got := roundRetail(test.value, true); got != test.want {
			t.Errorf("roundRetail(%v) = %d, want %d", test.value, got, test.want)
		}
	}
}

// Округление включается настройкой. Выключенное — это обычное округление
// до рубля, а не «как получится».
func TestRoundRetailCanBeSwitchedOff(t *testing.T) {
	if got := roundRetail(1234.4, false); got != 1234 {
		t.Fatalf("без округления цен = %d, ожидали 1234", got)
	}
	if got := roundRetail(1234.6, false); got != 1235 {
		t.Fatalf("без округления цен = %d, ожидали 1235", got)
	}
}

func TestDeliveryIsSplitAcrossParsedTrolleys(t *testing.T) {
	perTrolley := deliveryPerTrolley(160000, 3)
	if math.Abs(perTrolley*3-160000) > 0.000001 {
		t.Fatalf("allocated delivery = %.2f, want 160000", perTrolley*3)
	}
}

func TestAllocationCannotMakeLastRowNegative(t *testing.T) {
	weights := []allocationWeight{}
	for i := int64(1); i <= 6; i++ {
		weights = append(weights, allocationWeight{ID: i, Weight: 1, Quantity: 1})
	}
	result := allocateMoneyPerUnit(.04, weights)
	total := 0.
	for _, v := range result {
		if v < 0 {
			t.Fatalf("negative allocated cost: %f", v)
		}
		total += v
	}
	if math.Abs(total-.04) > 1e-9 {
		t.Fatalf("lost kopeck: %.8f", total)
	}
}
