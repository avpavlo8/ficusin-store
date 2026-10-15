package procurement

import (
	"math"
	"testing"
)

func amount(v float64) *float64 { return &v }
func zeroCosts() ChannelCosts {
	return ChannelCosts{amount(0), amount(0), amount(0), amount(0), amount(0)}
}

func TestMarketplaceRetainsSameContributionAfterItsOwnDeductions(t *testing.T) {
	site := zeroCosts()
	site.AcquiringRate = amount(.02)
	wb := ChannelCosts{amount(.36), amount(0), amount(150), amount(100), amount(30)}
	ozon := ChannelCosts{amount(.52), amount(.01), amount(150), amount(200), amount(30)}
	settings := PricingSettings{TaxRate: .08, Economics: PricingEconomics{300000, map[string]ChannelCosts{"site": site, "wb": wb, "ozon": ozon}}}
	quotes := calculateChannelQuotes(settings, 500, 1000, nil)
	// Direct 1000 - 8% tax - 2% acquiring - 500 = 400.
	if *quotes["site"].ContributionRUB != 400 || *quotes["site"].CoverUnits != 750 {
		t.Fatalf("site=%+v", quotes["site"])
	}
	for channel, want := range map[string]int64{"wb": 2108, "ozon": 3283} {
		q := quotes[channel]
		if q.PriceRUB == nil || *q.PriceRUB != want {
			t.Fatalf("%s=%+v want %d", channel, q, want)
		}
		if *q.ContributionRUB < 400 || *q.ContributionRUB > 401 {
			t.Fatalf("lost contribution on %s: %f", channel, *q.ContributionRUB)
		}
	}
	// No second 25% reserve, height surcharge or notional strike price.
	settings.ReserveRate = .25
	settings.MarketplaceLogisticsPerCM = 999
	again := calculateChannelQuotes(settings, 500, 1000, nil)
	if *again["wb"].PriceRUB != *quotes["wb"].PriceRUB {
		t.Fatal("legacy reserve counted twice")
	}
}
func TestUnknownChargesBlockQuoteButVerifiedZeroIsAllowed(t *testing.T) {
	settings := PricingSettings{Economics: PricingEconomics{300000, map[string]ChannelCosts{"site": zeroCosts(), "wb": {}}}}
	q := calculateChannelQuotes(settings, 500, 1000, nil)
	if q["wb"].PriceRUB != nil || len(q["wb"].Missing) != 5 {
		t.Fatalf("unknown expenses treated as free: %+v", q["wb"])
	}
	q = calculateChannelQuotes(settings, 500, 1000, map[string]ChannelCosts{"wb": zeroCosts()})
	if q["wb"].PriceRUB == nil || *q["wb"].PriceRUB != 1000 {
		t.Fatalf("explicit zero rejected: %+v", q["wb"])
	}
}
func TestNoProfitableQuoteForLossMakingRetailOrImpossibleRates(t *testing.T) {
	settings := PricingSettings{TaxRate: .08, Economics: PricingEconomics{300000, map[string]ChannelCosts{"site": zeroCosts(), "wb": zeroCosts()}}}
	q := calculateChannelQuotes(settings, 990, 1000, nil)
	if q["site"].PriceRUB != nil || q["site"].ContributionRUB != nil || q["site"].CoverUnits != nil || q["wb"].PriceRUB != nil {
		t.Fatalf("loss presented as profitable: %+v", q)
	}
	if *q["site"].BreakEvenRUB != 1077 {
		t.Fatalf("loss floor=%d", *q["site"].BreakEvenRUB)
	}
	costs := zeroCosts()
	costs.CommissionRate = amount(.92)
	if validateChannelCosts(costs, .08) {
		t.Fatal("accepted deductions of 100%")
	}
	costs = zeroCosts()
	costs.LogisticsRUB = amount(math.Inf(1))
	if validateChannelCosts(costs, .08) {
		t.Fatal("accepted infinite tariff")
	}
	costs.LogisticsRUB = amount(math.NaN())
	if validateChannelCosts(costs, .08) {
		t.Fatal("accepted NaN")
	}
}
func TestSKUExpenseOverridesDoNotChangeOtherChannels(t *testing.T) {
	settings := PricingSettings{Economics: PricingEconomics{Channels: map[string]ChannelCosts{"site": zeroCosts(), "wb": zeroCosts(), "ozon": zeroCosts()}}}
	bulky := zeroCosts()
	bulky.LogisticsRUB = amount(900)
	q := calculateChannelQuotes(settings, 500, 1000, map[string]ChannelCosts{"ozon": bulky})
	if *q["ozon"].PriceRUB != 1900 || *q["wb"].PriceRUB != 1000 || *q["site"].PriceRUB != 1000 {
		t.Fatalf("overrides=%+v", q)
	}
}
func TestInvoiceDimensionMismatchRequiresPhysicalReview(t *testing.T) {
	pot, height := 12., 40.
	invoice := &AliasReview{PotDiameterCM: amount(6), HeightCM: amount(15)}
	if dimensionWarning(&pot, &height, invoice, false) == "" {
		t.Fatal("silently used planned dimensions")
	}
	if dimensionWarning(&pot, &height, invoice, true) != "" {
		t.Fatal("ignored explicit physical review")
	}
	if dimensionWarning(amount(6), amount(15), invoice, false) != "" {
		t.Fatal("equal dimensions rejected")
	}
}

func TestSupplierPackagingSeparatesExpenseFromRefundableDeposit(t *testing.T) {
	text := "Single Package 14,80\nReturnable Package 196,00\nPackage Rent 16,80\n"
	expense, deposit, err := supplierPackageCharges(text, 227.60)
	if err != nil || math.Abs(expense-31.60) > 1e-9 || deposit != 196 {
		t.Fatalf("expense=%v deposit=%v err=%v", expense, deposit, err)
	}
	if _, _, err = supplierPackageCharges(text, 250); err == nil {
		t.Fatal("silently dropped unexplained invoice packaging")
	}
	line := calculateAllocatedLine(PricingSettings{RetailMarkupMultiplier: 2}, KindInternational, 10, 100, 200, 50, 30, 31.60)
	if math.Abs(line.UnitCostRUB-1281.60) > 1e-9 || line.AdditionalPurchaseUnitRUB != 31.60 || line.PurchaseUnitRUB != 1000 {
		t.Fatalf("supplier charge missing or mixed into purchase: %+v", line)
	}
}

func TestDomesticDeliveryAndOtherPurchaseCostsAreNotLostWithoutDimensions(t *testing.T) {
	input := PlanCreate{Costs: &CalculationInput{ExchangeRate: 1, DeliveryToMoscowRUB: 100, DeliveryToRyazanRUB: 50, AdditionalCostsRUB: 30}, Items: []PlanItem{{Quantity: 2, ExpectedUnitPrice: 100}, {Quantity: 1, ExpectedUnitPrice: 200}}}
	lines, _, err := calculatePlan(PricingSettings{RetailMarkupMultiplier: 2}, KindDomestic, input)
	if err != nil {
		t.Fatal(err)
	}
	total := lines[0].UnitCostRUB*2 + lines[1].UnitCostRUB
	if math.Abs(total-580) > 1e-9 {
		t.Fatalf("delivery or other charges lost: %v", total)
	}
}
