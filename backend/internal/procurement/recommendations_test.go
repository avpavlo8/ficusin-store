package procurement

import "testing"

func TestRecommendationUsesAllDemandAndRoundsSupplierPack(t *testing.T) {
	item, ok := calculateRecommendation(recommendationInput{
		Recommendation: Recommendation{
			SiteSales: 10, SabySales: 10, WBSales: 5, OzonSales: 5,
			Balance: 5, MinimumOrderQty: 6, OrderMultiple: 6,
		},
		AvailabilityStatus: "available",
	}, 30, 30)
	if !ok || item.Status != RecommendationReady || item.TotalSales != 30 || item.SuggestedQty != 30 {
		t.Fatalf("recommendation = %+v, included = %v", item, ok)
	}
	if item.DaysOfCover == nil || *item.DaysOfCover != 5 {
		t.Fatalf("days of cover = %v, want 5", item.DaysOfCover)
	}
}

func TestRecommendationExcludesProductAlreadyInOpenOrder(t *testing.T) {
	item, ok := calculateRecommendation(recommendationInput{
		Recommendation:     Recommendation{TotalSales: 0, SiteSales: 12, Balance: 0, Incoming: 12, MinimumOrderQty: 1, OrderMultiple: 1},
		AvailabilityStatus: "available",
	}, 30, 30)
	if !ok || item.Status != RecommendationIncoming || item.SuggestedQty != 0 {
		t.Fatalf("recommendation = %+v, included = %v", item, ok)
	}
}

func TestCustomerDemandSurvivesInsufficientIncomingOrder(t *testing.T) {
	item, ok := calculateRecommendation(recommendationInput{
		Recommendation:     Recommendation{CustomerRequests: 3, Incoming: 2, MinimumOrderQty: 1, OrderMultiple: 1},
		AvailabilityStatus: "available",
	}, 30, 30)
	if !ok || item.Status != RecommendationReady || item.SuggestedQty != 1 {
		t.Fatalf("recommendation = %+v, included = %v", item, ok)
	}
}

func TestUnavailableDemandMovesToAvailabilityQueue(t *testing.T) {
	item, ok := calculateRecommendation(recommendationInput{
		Recommendation:     Recommendation{SabySales: 8, MinimumOrderQty: 1, OrderMultiple: 1},
		AvailabilityStatus: "check",
	}, 30, 30)
	if !ok || item.Status != RecommendationAvailability || item.SuggestedQty != 0 {
		t.Fatalf("recommendation = %+v, included = %v", item, ok)
	}
}

func TestStaffSuggestionWithoutSalesGoesToOrderQueue(t *testing.T) {
	item, ok := calculateRecommendation(recommendationInput{
		Recommendation:     Recommendation{StaffRequests: 2, MinimumOrderQty: 4, OrderMultiple: 4},
		AvailabilityStatus: "available",
	}, 60, 60)
	if !ok || item.Status != RecommendationReady || item.SuggestedQty != 4 {
		t.Fatalf("recommendation = %+v, included = %v", item, ok)
	}
}

func TestRecommendationReplacesPeriodSalesLessCurrentStock(t *testing.T) {
	item, ok := calculateRecommendation(recommendationInput{
		Recommendation:     Recommendation{SabySales: 10, Balance: 4, MinimumOrderQty: 1, OrderMultiple: 1},
		AvailabilityStatus: "available",
	}, 60, 60)
	if !ok || item.Status != RecommendationReady || item.SuggestedQty != 6 {
		t.Fatalf("recommendation = %+v, included = %v", item, ok)
	}
}

func TestNoStockAndNoSalesStaysInOrderQueue(t *testing.T) {
	item, ok := calculateRecommendation(recommendationInput{
		Recommendation:     Recommendation{Balance: 0, MinimumOrderQty: 6, OrderMultiple: 6},
		AvailabilityStatus: "available",
	}, 60, 60)
	if !ok || item.Status != RecommendationReady || item.SuggestedQty != 0 || item.QuantityKnown {
		t.Fatalf("recommendation = %+v, included = %v", item, ok)
	}
}

func TestStage05IncomingOnlySubtractsCoveredRemainder(t *testing.T) {
	item, ok := calculateRecommendation(recommendationInput{Recommendation: Recommendation{
		SabySales: 30, Balance: 0, Incoming: 5, MinimumOrderQty: 1, OrderMultiple: 1,
	}, AvailabilityStatus: "available"}, 30, 30)
	if !ok || item.Status != RecommendationReady || item.NeedBeforeIncoming != 30 || item.UncoveredQty != 25 || item.SuggestedQty != 25 {
		t.Fatalf("recommendation=%+v included=%v", item, ok)
	}
}

func TestStage05SeasonalHistoryAndCoverAreIndependent(t *testing.T) {
	item, ok := calculateRecommendation(recommendationInput{Recommendation: Recommendation{
		SiteSales: 60, Balance: 10, MinimumOrderQty: 1, OrderMultiple: 1,
	}, AvailabilityStatus: "available"}, 60, 45)
	if !ok || item.DemandQty != 45 || item.SuggestedQty != 35 {
		t.Fatalf("recommendation=%+v included=%v", item, ok)
	}
}

func TestStage05UnknownBalanceDoesNotInventQuantity(t *testing.T) {
	item, ok := calculateRecommendation(recommendationInput{Recommendation: Recommendation{
		SabySales: 12, MinimumOrderQty: 6, OrderMultiple: 6, BalanceKnown: false,
	}, AvailabilityStatus: "available", BalanceStateProvided: true}, 60, 45)
	if !ok || item.Status != RecommendationReady || item.QuantityKnown || item.SuggestedQty != 0 {
		t.Fatalf("recommendation=%+v included=%v", item, ok)
	}
}

func TestStage05PartialRequestCoverageUsesOnlyFreeIncoming(t *testing.T) {
	item, ok := calculateRecommendation(recommendationInput{Recommendation: Recommendation{
		CustomerRequests: 1, AllocatedRequests: 1, Incoming: 1, MinimumOrderQty: 1, OrderMultiple: 1,
	}, AvailabilityStatus: "available"}, 60, 45)
	if !ok || item.SuggestedQty != 1 || item.Status != RecommendationReady {
		t.Fatalf("recommendation=%+v included=%v", item, ok)
	}
}

func TestStage05FullyAllocatedRequestMovesToIncomingQueue(t *testing.T) {
	item, ok := calculateRecommendation(recommendationInput{Recommendation: Recommendation{
		AllocatedRequests: 2, Incoming: 2, MinimumOrderQty: 1, OrderMultiple: 1,
	}, AvailabilityStatus: "available"}, 60, 45)
	if !ok || item.Status != RecommendationIncoming {
		t.Fatalf("recommendation=%+v included=%v", item, ok)
	}
}

func TestStage05ExplainsSupplierRounding(t *testing.T) {
	item, ok := calculateRecommendation(recommendationInput{Recommendation: Recommendation{
		SabySales: 7, MinimumOrderQty: 6, OrderMultiple: 6,
	}, AvailabilityStatus: "available"}, 60, 60)
	if !ok || item.SuggestedQty != 12 || item.RoundingExplanation == "" {
		t.Fatalf("recommendation=%+v included=%v", item, ok)
	}
}

func TestMissingAtSupplierLeavesTheOrderQueue(t *testing.T) {
	item, ok := calculateRecommendation(recommendationInput{
		Recommendation:     Recommendation{Balance: 0, MinimumOrderQty: 1, OrderMultiple: 1},
		AvailabilityStatus: "temporarily_unavailable",
	}, 60, 60)
	if !ok || item.Status != RecommendationUnavailable || item.SuggestedQty != 0 {
		t.Fatalf("recommendation = %+v, included = %v", item, ok)
	}
}

// Горизонт закупки отвязан от периода истории: за шестьдесят дней продано
// десять, берём запас на тридцать — нужно пять, минус остаток.
func TestCoverDaysScaleDemandIndependentlyOfHistory(t *testing.T) {
	item, ok := calculateRecommendation(recommendationInput{
		Recommendation:     Recommendation{SabySales: 10, Balance: 1, MinimumOrderQty: 1, OrderMultiple: 1},
		AvailabilityStatus: "available",
	}, 60, 30)
	if !ok || item.Status != RecommendationReady || item.SuggestedQty != 4 {
		t.Fatalf("recommendation = %+v, included = %v", item, ok)
	}
}

// Снятое с закупки видно, но в заказ не идёт: человек должен понимать, от
// какого спроса он отказался.
func TestExcludedProductNeverReachesTheOrderQueue(t *testing.T) {
	item, ok := calculateRecommendation(recommendationInput{
		Recommendation:     Recommendation{SabySales: 40, Balance: 0, MinimumOrderQty: 1, OrderMultiple: 1},
		AvailabilityStatus: "available",
		Excluded:           true,
		ExclusionReason:    "Плохо приживается",
	}, 60, 60)
	if !ok || item.Status != RecommendationExcluded || item.SuggestedQty != 0 {
		t.Fatalf("recommendation = %+v, included = %v", item, ok)
	}
	if item.Reason != "Плохо приживается" {
		t.Fatalf("reason = %q", item.Reason)
	}
}

func TestUnknownSupplierAvailabilityDoesNotHideDemand(t *testing.T) {
	item, ok := calculateRecommendation(recommendationInput{
		Recommendation:     Recommendation{SabySales: 10, Balance: 4, MinimumOrderQty: 1, OrderMultiple: 1},
		AvailabilityStatus: "unknown",
	}, 60, 60)
	if !ok || item.Status != RecommendationReady || item.SuggestedQty != 6 {
		t.Fatalf("recommendation = %+v, included = %v", item, ok)
	}
}
