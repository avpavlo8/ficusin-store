package procurement

import (
	"math"
	"testing"
)

func TestCalculateInternationalLineUsesConfiguredRules(t *testing.T) {
	settings := PricingSettings{
		TrolleyVolumeCM3: 1, TrolleyFillRatio: 1,
		RetailMarkupMultiplier: 2.1,
	}
	result := calculateLine(settings, OrderCosts{ExchangeRate: 2}, calculationLine{
		Kind: KindInternational, Quantity: 1, UnitPrice: 10,
	}, 0)
	if result.ProposedRetailRUB != 42 {
		t.Fatalf("retail = %d, want 42", result.ProposedRetailRUB)
	}
}

func TestCalculateDomesticLineUsesRublePurchasePrice(t *testing.T) {
	settings := PricingSettings{
		RetailMarkupMultiplier: 2,
	}
	result := calculateLine(settings, OrderCosts{}, calculationLine{Kind: KindDomestic, UnitPrice: 450}, 0)
	if result.PurchaseUnitRUB != 450 || result.ProposedRetailRUB != 900 {
		t.Fatalf("domestic calculation = %#v", result)
	}
}

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
	result := calculateAllocatedLine(settings, KindInternational, 10, 100, 250, 50)
	if result.PurchaseUnitRUB != 1000 || result.TrolleyDeliveryUnitRUB != 250 || result.RyazanDeliveryUnitRUB != 50 || result.UnitCostRUB != 1300 {
		t.Fatalf("allocated calculation = %#v", result)
	}
	if result.ProposedRetailRUB != 2600 {
		t.Fatalf("retail = %d, want 2600", result.ProposedRetailRUB)
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

func TestDeliveryIsSplitAcrossParsedTrolleys(t *testing.T) {
	perTrolley := deliveryPerTrolley(160000, 3)
	if math.Abs(perTrolley*3-160000) > 0.000001 {
		t.Fatalf("allocated delivery = %.2f, want 160000", perTrolley*3)
	}
}
