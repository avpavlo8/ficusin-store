package procurement

import (
	"fmt"
	"math"
)

// Очереди экрана рекомендаций.
//
// «К заказу» — единственный список, по которому формируется заказ
// поставщику. Товар без остатка и ручная рекомендация магазина живут здесь
// же: разбирать их по отдельным вкладкам значило заставлять человека
// собирать заказ из трёх мест. Остальные очереди — это то, что из «К
// заказу» вычли, и каждая называет причину.
const (
	RecommendationReady        = "recommended"
	RecommendationIncoming     = "already_ordered"
	RecommendationAvailability = "check_availability"
	RecommendationUnavailable  = "supplier_unavailable"
	RecommendationExcluded     = "excluded"
)

type recommendationInput struct {
	Recommendation
	AvailabilityStatus string
	Excluded           bool
	ExclusionReason    string
}

// calculateRecommendation считает потребность и раскладывает товар по
// очередям.
//
// historyDays — за сколько дней смотрим продажи, coverDays — на сколько
// дней берём запас. Раньше это было одно число, и длинный период истории
// молча превращался в такой же длинный заказ.
func calculateRecommendation(input recommendationInput, historyDays, coverDays int) (Recommendation, bool) {
	item := input.Recommendation
	item.Availability = input.AvailabilityStatus
	item.SiteSales = max(0, item.SiteSales)
	item.SabySales = max(0, item.SabySales)
	item.WBSales = max(0, item.WBSales)
	item.OzonSales = max(0, item.OzonSales)
	item.TotalSales = item.SiteSales + item.SabySales + item.WBSales + item.OzonSales
	item.OpenRequests = item.CustomerRequests + item.StaffRequests
	if historyDays <= 0 {
		return Recommendation{}, false
	}
	if coverDays <= 0 {
		coverDays = historyDays
	}
	item.DailySales = float64(item.TotalSales) / float64(historyDays)
	if item.DailySales > 0 {
		cover := float64(max(0, item.Balance)) / item.DailySales
		item.DaysOfCover = &cover
	}

	// Решение магазина не закупать сильнее любого расчёта: считать спрос по
	// такому товару всё равно нужно, чтобы человек видел, от чего
	// отказывается, но в заказ он не попадёт.
	if input.Excluded {
		item.Status = RecommendationExcluded
		item.Reason = defaultReason(input.ExclusionReason, "Снят с закупки решением магазина")
		return item, true
	}

	// Считаем целыми: 10 продаж за 60 дней с горизонтом 60 в дробной
	// арифметике дают 9.999999999999998, и округление вверх молча
	// превращается то в 10, то в 11.
	demand := (item.TotalSales*coverDays+historyDays-1)/historyDays + item.OpenRequests
	needBeforeIncoming := max(0, demand-max(0, item.Balance))

	// Нулевой остаток без продаж и без заявок — это не рассчитанная
	// потребность, а напоминание: растения нет ни на складе, ни в продаже.
	// Просим минимальную партию.
	if needBeforeIncoming == 0 && item.Balance <= 0 && item.TotalSales == 0 && item.OpenRequests == 0 {
		needBeforeIncoming = 1
	}
	if needBeforeIncoming == 0 {
		return Recommendation{}, false
	}

	switch input.AvailabilityStatus {
	case "check":
		item.Status = RecommendationAvailability
		item.Reason = "Наличие у поставщика нужно проверить"
		return item, true
	case "temporarily_unavailable":
		item.Status = RecommendationUnavailable
		item.Reason = "У поставщика временно нет"
		return item, true
	case "discontinued":
		item.Status = RecommendationUnavailable
		item.Reason = "Поставщик снял с продажи"
		return item, true
	}

	// Заказанное и ещё не приехавшее закрывает потребность, иначе одно и то
	// же растение уедет в заказ дважды.
	rawQuantity := max(0, needBeforeIncoming-max(0, item.Incoming))
	// Новая заявка клиента не должна исчезнуть только потому, что в пути
	// есть старый заказ, которого на этого клиента не хватает.
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
	item.Status = RecommendationReady
	switch {
	case item.CustomerRequests > 0:
		item.Reason = "Есть товар под заказ клиента"
	case item.TotalSales == 0 && item.StaffRequests > 0:
		item.Reason = "Ручная рекомендация для ассортимента магазина"
	case item.TotalSales == 0:
		item.Reason = "Нет остатка и продаж за выбранный период"
	default:
		item.Reason = fmt.Sprintf("За %d дн. продано %d шт., на остатке %d шт.; закупаем запас на %d дн.",
			historyDays, item.TotalSales, max(0, item.Balance), coverDays)
	}
	return item, true
}

func defaultReason(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
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
	case RecommendationIncoming:
		return 2
	case RecommendationAvailability:
		return 3
	case RecommendationUnavailable:
		return 4
	case RecommendationExcluded:
		return 5
	default:
		return 6
	}
}
