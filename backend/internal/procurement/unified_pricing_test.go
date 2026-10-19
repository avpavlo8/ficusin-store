package procurement

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

func unifiedSettings(t *testing.T) PricingSettings {
	t.Helper()
	raw, err := os.ReadFile("unified_defaults.json")
	if err != nil {
		t.Fatal(err)
	}
	var s PricingSettings
	if err = json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	return s
}
func TestUnifiedPricingRetainsSameGrowingContribution(t *testing.T) {
	s := unifiedSettings(t)
	box := &PricingPackage{LengthCM: 15, WidthCM: 15, HeightCM: 40}
	for _, cost := range []float64{137.697667, 755.111, 2631.5775, 10000} {
		q := UnifiedChannelQuotes(s, cost, box)
		for _, ch := range []string{"site", "wb", "ozon"} {
			v := q[ch]
			if v.PriceRUB == nil || v.ContributionRUB == nil {
				t.Fatalf("%s: %+v", ch, v)
			}
			if *v.ContributionRUB < cost*.6 || *v.ContributionRUB-cost*.6 >= 1 {
				t.Fatalf("%s contribution=%v target=%v", ch, *v.ContributionRUB, cost*.6)
			}
			// Price is the minimum whole ruble that covers the target.
			if float64(*v.PriceRUB-1)*retainedRate(s.TaxRate, *v.Costs)-cost-*v.Costs.PackagingRUB-*v.Costs.LogisticsRUB-*v.Costs.LossRUB >= cost*.6 {
				t.Fatal("price unnecessarily inflated")
			}
		}
		if math.Abs(*q["wb"].ContributionRUB-*q["ozon"].ContributionRUB) >= 1 {
			t.Fatal("different target between MPs")
		}
	}
}
func TestUnifiedIgnoresManualPricesAndSiteAnchor(t *testing.T) {
	s := unifiedSettings(t)
	q := calculateChannelQuotes(s, 1000, 1, map[string]ChannelCosts{"site": {}}, map[string]int64{"wb": 99999})
	if q["site"].PriceRUB == nil || *q["site"].ContributionRUB < 600 {
		t.Fatal("retail anchor changed target")
	}
	if q["wb"].PriceRUB != nil || q["wb"].Manual {
		t.Fatal("manual override bypassed missing box")
	}
}
func TestUnifiedBoxCostAndFractionalLiters(t *testing.T) {
	s := unifiedSettings(t)
	for _, tc := range []struct {
		box                 PricingPackage
		pack, forward, ozon float64
	}{
		{PricingPackage{LengthCM: 10, WidthCM: 10, HeightCM: 15}, 130, 100.1, 209.96542},
		{PricingPackage{LengthCM: 15, WidthCM: 15, HeightCM: 40}, 145, 278.6, 256.3517533333333},
		{PricingPackage{LengthCM: 20, WidthCM: 20, HeightCM: 80}, 160, 826, 269.3766666666666},
	} {
		q := UnifiedChannelQuotes(s, 1000, &tc.box)
		if math.Abs(*q["wb"].Costs.LogisticsRUB-tc.forward) > 1e-8 || *q["wb"].Costs.PackagingRUB != tc.pack || math.Abs(*q["ozon"].Costs.LogisticsRUB-tc.ozon) > 1e-8 {
			t.Fatalf("wrong size group: %+v", q)
		}
	}
}
func TestUnifiedCannotTreatMissingBoxOrInvalidRatesAsFree(t *testing.T) {
	s := unifiedSettings(t)
	if UnifiedChannelQuotes(s, 1000, nil)["ozon"].PriceRUB != nil {
		t.Fatal("missing package accepted")
	}
	huge := &PricingPackage{LengthCM: 100, WidthCM: 100, HeightCM: 100}
	if UnifiedChannelQuotes(s, 1000, huge)["ozon"].PriceRUB != nil {
		t.Fatal("unknown large-box tariff invented")
	}
	s.Economics.Channels["ozon"] = ChannelCosts{CommissionRate: amount(.91), AcquiringRate: amount(.01), OtherRate: amount(.015), PackagingRUB: amount(0), LogisticsRUB: amount(0), LossRUB: amount(100)}
	if validateUnified(s.Economics.Unified, s.Economics.Channels, .08) {
		t.Fatal("deductions above 100% accepted")
	}
	if UnifiedChannelQuotes(s, 1000, huge)["site"].PriceRUB != nil {
		t.Fatal("invalid settings allowed")
	}
}
