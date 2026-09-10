package procurement

import (
	"math"
	"testing"
)

func TestPlanCalculationReusesInvoicePricingAndAllocatesAllDelivery(t *testing.T) {
	pot1, pot2, height := 10.0, 20.0, 50.0
	input := PlanCreate{Costs: &CalculationInput{ExchangeRate: 100, DeliveryToMoscowRUB: 600, DeliveryToRyazanRUB: 300}, Items: []PlanItem{
		{Quantity: 2, ExpectedUnitPrice: 5, PotDiameterCM: &pot1, HeightCM: &height, LoadUnit: "shelf"},
		{Quantity: 4, ExpectedUnitPrice: 10, PotDiameterCM: &pot2, HeightCM: &height, LoadUnit: "shelf"},
	}}
	settings := PricingSettings{RetailMarkupMultiplier: 2, RoundPrices: true}
	lines, preview, err := calculatePlan(settings, KindInternational, input)
	if err != nil { t.Fatal(err) }
	moscow, ryazan := 0.0, 0.0
	for i, line := range lines {
		quantity := float64(input.Items[i].Quantity)
		moscow += line.TrolleyDeliveryUnitRUB * quantity
		ryazan += line.RyazanDeliveryUnitRUB * quantity
		want := calculateAllocatedLine(settings, KindInternational, input.Items[i].ExpectedUnitPrice, 100, line.TrolleyDeliveryUnitRUB, line.RyazanDeliveryUnitRUB, height)
		if line != want || preview.Lines[i].Retail != want.ProposedRetailRUB { t.Fatalf("pricing mismatch at %d", i) }
	}
	if math.Abs(moscow-600) > 0.000001 || math.Abs(ryazan-300) > 0.000001 { t.Fatalf("lost logistics: %f %f", moscow, ryazan) }
	if math.Abs(lines[1].TrolleyDeliveryUnitRUB/lines[0].TrolleyDeliveryUnitRUB-4) > 0.000001 { t.Fatal("volume weighting lost") }
}

func TestPlanCalculationRejectsMissingDimensionsAndNonFiniteCosts(t *testing.T) {
	height, pot := 50.0, 10.0
	input := PlanCreate{Costs: &CalculationInput{ExchangeRate: 100}, Items: []PlanItem{{Quantity: 1, ExpectedUnitPrice: 5, HeightCM: &height}}}
	if _, _, err := calculatePlan(PricingSettings{}, KindInternational, input); err == nil { t.Fatal("accepted missing pot") }
	input.Items[0].PotDiameterCM = &pot
	input.Costs.ExchangeRate = math.Inf(1)
	if _, _, err := calculatePlan(PricingSettings{}, KindInternational, input); err == nil { t.Fatal("accepted infinite exchange rate") }
}
