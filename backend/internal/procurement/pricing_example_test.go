package procurement

import (
	"math"
	"testing"
)

func TestUnitPriceModes(t *testing.T) {
	s := unifiedSettings(t)
	box := &PricingPackage{LengthCM: 15, WidthCM: 15, HeightCM: 40}
	markup := .75
	cost := 1000.25
	got, err := previewUnitPrice(s, UnitPriceInput{Mode: "cost", CostRUB: cost, MarkupRate: &markup, Package: box})
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range []string{"site", "wb", "ozon"} {
		q := got.Quotes[ch]
		if q.PriceRUB == nil || q.ContributionRUB == nil || *q.ContributionRUB < cost*markup || *q.ContributionRUB-cost*markup >= 1 {
			t.Fatalf("%s: %+v", ch, q)
		}
	}
	if s.Economics.Unified.MarkupRate != .6 {
		t.Fatal("preview mutated global settings")
	}
	retail := got.RetailRUB + .37
	translated, err := previewUnitPrice(s, UnitPriceInput{Mode: "retail", RetailRUB: retail, Package: box})
	if err != nil || translated.RetailRUB != retail {
		t.Fatalf("retail changed: %+v %v", translated, err)
	}
	site := s.Economics.Channels["site"]
	net := retail*retainedRate(s.TaxRate, site) - *site.PackagingRUB - *site.LogisticsRUB - *site.LossRUB
	for _, ch := range []string{"wb", "ozon"} {
		q := translated.Quotes[ch]
		if q.PriceRUB == nil || q.ContributionRUB != nil || q.BreakEvenRUB != nil || q.CoverUnits != nil {
			t.Fatalf("invented profitability: %+v", q)
		}
		c := q.Costs
		actual := float64(*q.PriceRUB)*retainedRate(s.TaxRate, *c) - *c.PackagingRUB - *c.LogisticsRUB - *c.LossRUB
		if actual < net || actual-net >= 1 {
			t.Fatalf("different net receipts: %g %g", actual, net)
		}
	}
	zero := 0.
	if _, err = previewUnitPrice(s, UnitPriceInput{Mode: "cost", CostRUB: cost, MarkupRate: &zero, Package: box}); err != nil {
		t.Fatal(err)
	}
	noBox, err := previewUnitPrice(s, UnitPriceInput{Mode: "retail", RetailRUB: 2000})
	if err != nil || noBox.Quotes["wb"].PriceRUB != nil || len(noBox.Quotes["wb"].Missing) == 0 {
		t.Fatalf("missing package accepted: %+v %v", noBox, err)
	}
	small, _ := previewUnitPrice(s, UnitPriceInput{Mode: "retail", RetailRUB: 2000, Package: &PricingPackage{LengthCM: 10, WidthCM: 10, HeightCM: 10}})
	large, _ := previewUnitPrice(s, UnitPriceInput{Mode: "retail", RetailRUB: 2000, Package: &PricingPackage{LengthCM: 20, WidthCM: 20, HeightCM: 80}})
	if *small.Quotes["wb"].PriceRUB >= *large.Quotes["wb"].PriceRUB {
		t.Fatal("parcel size ignored")
	}

}

func TestUnitPriceValidation(t *testing.T) {
	negative := -1.
	for _, input := range []UnitPriceInput{
		{CostRUB: 100, RetailRUB: 200},
		{Mode: "unknown", CostRUB: 100, RetailRUB: 200},
		{Mode: "cost", CostRUB: 0}, {Mode: "retail", RetailRUB: 0},
		{Mode: "cost", CostRUB: math.Inf(1)}, {Mode: "retail", RetailRUB: math.NaN()},
		{Mode: "cost", CostRUB: 100, MarkupRate: &negative},
		{Mode: "cost", CostRUB: 100, Package: &PricingPackage{LengthCM: -1, WidthCM: 10, HeightCM: 10}},
	} {
		if validateUnitPriceInput(input) == nil {
			t.Fatalf("accepted %+v", input)
		}
	}
}

func TestExplicitFallbackPriceModesKeepDifferentEconomicMeanings(t *testing.T) {
	direct, marketplace := zeroCosts(), zeroCosts()
	marketplace.CommissionRate, marketplace.LogisticsRUB = amount(.2), amount(100)
	settings := PricingSettings{RetailMarkupMultiplier: 2, Economics: PricingEconomics{Channels: map[string]ChannelCosts{"site": direct, "wb": marketplace, "ozon": marketplace}}}
	markup := 1.
	cost, err := previewUnitPrice(settings, UnitPriceInput{Mode: "cost", CostRUB: 1000, MarkupRate: &markup})
	if err != nil {
		t.Fatal(err)
	}
	if cost.RetailRUB != 2000 || cost.Quotes["wb"].PriceRUB == nil || *cost.Quotes["wb"].PriceRUB != 2625 || cost.Quotes["wb"].ContributionRUB == nil || *cost.Quotes["wb"].ContributionRUB != 1000 {
		t.Fatalf("cost mode lost purchase cost/contribution: %+v", cost)
	}
	retail, err := previewUnitPrice(settings, UnitPriceInput{Mode: "retail", RetailRUB: 2100})
	if err != nil {
		t.Fatal(err)
	}
	if retail.RetailRUB != 2100 || retail.Quotes["wb"].PriceRUB == nil || *retail.Quotes["wb"].PriceRUB != 2750 || retail.Quotes["wb"].ContributionRUB != nil || retail.Quotes["wb"].BreakEvenRUB != nil {
		t.Fatalf("retail transfer inferred unknown purchase cost/profit: %+v", retail)
	}
}
