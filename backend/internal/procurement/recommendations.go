package procurement

import (
	"fmt"
	"math"
)

const (
	RecommendationReady        = "recommended"
	RecommendationIncoming     = "already_ordered"
	RecommendationAvailability = "check_availability"
	RecommendationNew          = "new_assortment"
)

type recommendationInput struct {
	Recommendation
	AvailabilityStatus string
}

func calculateRecommendation(input recommendationInput, historyDays, targetDays int) (Recommendation, bool) {
	item := input.Recommendation
	item.Availability = input.AvailabilityStatus
	item.SiteSales = max(0, item.SiteSales)
	item.SabySales = max(0, item.SabySales)
	item.WBSales = max(0, item.WBSales)
	item.OzonSales = max(0, item.OzonSales)
	item.TotalSales = item.SiteSales + item.SabySales + item.WBSales + item.OzonSales
	item.OpenRequests = item.CustomerRequests + item.StaffRequests
	if historyDays <= 0 || targetDays <= 0 {
		return Recommendation{}, false
	}
	item.DailySales = float64(item.TotalSales) / float64(historyDays)
	if item.DailySales > 0 {
		cover := float64(max(0, item.Balance)) / item.DailySales
		item.DaysOfCover = &cover
	}
	salesNeed := int(math.Ceil(item.DailySales * float64(targetDays)))
	needBeforeIncoming := max(0, salesNeed+item.OpenRequests-max(0, item.Balance))
	if needBeforeIncoming == 0 {
		return Recommendation{}, false
	}

	if input.AvailabilityStatus != "available" {
		item.Status = RecommendationAvailability
		item.Reason = "Есть спрос, но наличие у поставщика не подтверждено"
		return item, true
	}

	rawQuantity := max(0, needBeforeIncoming-max(0, item.Incoming))
	// A new customer order must not disappear merely because an older order is
	// on the way and cannot cover that customer.
	uncoveredCustomer := max(0, item.CustomerRequests-max(0, item.Balance)-max(0, item.Incoming))
	if item.Incoming > 0 && uncoveredCustomer == 0 {
		item.Status = RecommendationIncoming
		item.Reason = fmt.Sprintf("Уже заказано %d шт.; повторная закупка исключена", item.Incoming)
		return item, true
	}
	if rawQuantity == 0 {
		return Recommendation{}, false
	}

	item.SuggestedQty = roundOrderQuantity(rawQuantity, item.MinimumOrderQty, item.OrderMultiple)
	if item.StaffRequests > 0 && item.TotalSales == 0 {
		item.Status = RecommendationNew
		item.Reason = "Ручная рекомендация для ассортимента магазина"
	} else if item.CustomerRequests > 0 {
		item.Status = RecommendationReady
		item.Reason = "Есть товар под заказ клиента"
	} else {
		item.Status = RecommendationReady
		item.Reason = fmt.Sprintf("Запаса меньше чем на %d дней", targetDays)
	}
	return item, true
}

func roundOrderQuantity(quantity, minimum, multiple int) int {
	minimum = max(1, minimum)
	multiple = max(1, multiple)
	quantity = max(quantity, minimum)
	return int(math.Ceil(float64(quantity)/float64(multiple))) * multiple
}

func recommendationPriority(item Recommendation) int {
	switch item.Status {
	case RecommendationReady:
		if item.CustomerRequests > 0 {
			return 0
		}
		return 1
	case RecommendationNew:
		return 2
	case RecommendationIncoming:
		return 3
	case RecommendationAvailability:
		return 4
	default:
		return 5
	}
}
