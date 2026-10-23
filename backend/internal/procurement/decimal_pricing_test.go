package procurement

import "testing"

func TestLegacyPricingKeepsBothRoundingStages(t *testing.T) {
	for _, tt := range []struct {
		minor                                                          int64
		pack, height, logistics, returns, market, tax, reserve, strike float64
		want, compare                                                  int64
	}{
		{100, 0, 0, 0, .1, .2, .6, .1, .15, 2, 2},
		{10000, 0, 0, 0, 0, 0, 0, 0, .15, 100, 115},
		{10001, .29, 10, .1, .1, .2, .06, .04, .15, 141, 162},
	} {
		got, compare := LegacyMarketplacePrices(tt.minor, tt.pack, tt.height, tt.logistics, tt.returns, tt.market, tt.tax, tt.reserve, tt.strike)
		if got != tt.want || compare != tt.compare {
			t.Fatalf("%+v: got %d/%d", tt, got, compare)
		}
	}
	if got := CompareAtPrice(100, .1); got != 110 {
		t.Fatalf("binary ceil added ruble: %d", got)
	}
}
func TestPriceThresholdExactBoundary(t *testing.T) {
	if priceChangeNeeded(1.25, 1, .2) {
		t.Fatal("exactly 20 percent must not cross threshold")
	}
	if !priceChangeNeeded(1.25, 1, .199999) {
		t.Fatal("change above threshold missed")
	}
	if roundRetail(120, true) != 150 || roundRetail(120.000000001, true) != 150 || roundRetail(119.999999999, true) != 90 {
		t.Fatal("50/90 tie policy changed")
	}
}
func TestDecimalCoverageDoesNotAddSale(t *testing.T) {
	c := ChannelCosts{CommissionRate: amount(0), AcquiringRate: amount(0), PackagingRUB: amount(0), LogisticsRUB: amount(0), LossRUB: amount(0)}
	q := calculateChannelQuotes(PricingSettings{Economics: PricingEconomics{Channels: map[string]ChannelCosts{"site": c, "wb": c, "ozon": c}, MonthlyFixedCostsRUB: .21}}, .93, 1, nil)
	if q["site"].CoverUnits == nil || *q["site"].CoverUnits != 3 {
		t.Fatalf("coverage %+v", q["site"])
	}
}
