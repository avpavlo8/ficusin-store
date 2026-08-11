package procurement

import "testing"

func TestCalculateInternationalLineUsesConfiguredRules(t *testing.T) {
	settings := PricingSettings{
		TrolleyVolumeCM3: 1, TrolleyFillRatio: 1,
		InternationalCostMultiplier: 2, InternationalRetailMultiplier: 1.1,
		DomesticRetailMultiplier: 2, RetailRoundStep: 10,
	}
	result := calculateLine(settings, OrderCosts{ExchangeRate: 2}, calculationLine{
		Kind: KindInternational, Quantity: 1, UnitPrice: 10,
	}, 0)
	if result.ProposedRetailRUB != 50 {
		t.Fatalf("retail = %d, want 50", result.ProposedRetailRUB)
	}
}

func TestCalculateDomesticLineUsesRublePurchasePrice(t *testing.T) {
	settings := PricingSettings{
		DomesticRetailMultiplier: 2, RetailRoundStep: 50,
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
		InternationalCostMultiplier: 2, InternationalRetailMultiplier: 1.1,
		DomesticRetailMultiplier: 2, RetailRoundStep: 10,
	}
	result := calculateAllocatedLine(settings, KindInternational, 10, 100, 250, 50)
	if result.PurchaseUnitRUB != 1000 || result.TrolleyDeliveryUnitRUB != 250 || result.RyazanDeliveryUnitRUB != 50 || result.UnitCostRUB != 1300 {
		t.Fatalf("allocated calculation = %#v", result)
	}
}
