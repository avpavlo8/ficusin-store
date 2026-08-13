package procurement

import "testing"

func TestRecommendationUsesAllDemandAndRoundsSupplierPack(t *testing.T) {
	item, ok := calculateRecommendation(recommendationInput{
		Recommendation: Recommendation{
			SiteSales: 10, SabySales: 10, WBSales: 5, OzonSales: 5,
			Balance: 5, MinimumOrderQty: 6, OrderMultiple: 6,
		},
		AvailabilityStatus: "available",
	}, 30)
	if !ok || item.Status != RecommendationReady || item.TotalSales != 30 || item.SuggestedQty != 30 {
		t.Fatalf("recommendation = %+v, included = %v", item, ok)
	}
	if item.DaysOfCover == nil || *item.DaysOfCover != 5 {
		t.Fatalf("days of cover = %v, want 5", item.DaysOfCover)
	}
}

func TestRecommendationExcludesProductAlreadyInOpenOrder(t *testing.T) {
	item, ok := calculateRecommendation(recommendationInput{
		Recommendation: Recommendation{TotalSales: 0, SiteSales: 12, Balance: 0, Incoming: 12, MinimumOrderQty: 1, OrderMultiple: 1},
		AvailabilityStatus: "available",
	}, 30)
	if !ok || item.Status != RecommendationIncoming || item.SuggestedQty != 0 {
		t.Fatalf("recommendation = %+v, included = %v", item, ok)
	}
}

func TestCustomerDemandSurvivesInsufficientIncomingOrder(t *testing.T) {
	item, ok := calculateRecommendation(recommendationInput{
		Recommendation: Recommendation{CustomerRequests: 3, Incoming: 2, MinimumOrderQty: 1, OrderMultiple: 1},
		AvailabilityStatus: "available",
	}, 30)
	if !ok || item.Status != RecommendationReady || item.SuggestedQty != 1 {
		t.Fatalf("recommendation = %+v, included = %v", item, ok)
	}
}

func TestUnavailableDemandMovesToAvailabilityQueue(t *testing.T) {
	item, ok := calculateRecommendation(recommendationInput{
		Recommendation: Recommendation{SabySales: 8, MinimumOrderQty: 1, OrderMultiple: 1},
		AvailabilityStatus: "check",
	}, 30)
	if !ok || item.Status != RecommendationAvailability || item.SuggestedQty != 0 {
		t.Fatalf("recommendation = %+v, included = %v", item, ok)
	}
}

func TestStaffSuggestionWithoutSalesIsNewAssortment(t *testing.T) {
	item, ok := calculateRecommendation(recommendationInput{
		Recommendation: Recommendation{StaffRequests: 2, MinimumOrderQty: 4, OrderMultiple: 4},
		AvailabilityStatus: "available",
	}, 60)
	if !ok || item.Status != RecommendationNew || item.SuggestedQty != 4 {
		t.Fatalf("recommendation = %+v, included = %v", item, ok)
	}
}

func TestRecommendationReplacesPeriodSalesLessCurrentStock(t *testing.T) {
	item, ok := calculateRecommendation(recommendationInput{
		Recommendation: Recommendation{SabySales: 10, Balance: 4, MinimumOrderQty: 1, OrderMultiple: 1},
		AvailabilityStatus: "available",
	}, 60)
	if !ok || item.Status != RecommendationReady || item.SuggestedQty != 6 {
		t.Fatalf("recommendation = %+v, included = %v", item, ok)
	}
}
