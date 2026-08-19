package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/avpavlo8/ficusin-store/backend/internal/settings"
)

// deliveryFeesHandler отдаёт витрине цену простой доставки.
//
// Раньше 490 и 590 были вписаны в двух местах сразу: в браузере и в
// оформлении заказа. Стоило поменять цену — покупатель видел одно число, а
// счёт получал на другое. Теперь число одно и приходит оно отсюда.
//
// Ручка открытая: цена доставки и так написана на странице оформления, и
// прятать её не от кого.
func deliveryFeesHandler(shopSettings settingsService) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		fees := map[string]int{
			"courier": settings.NonNegative(settings.DefaultNumber(settings.CourierFee)),
			"post":    settings.NonNegative(settings.DefaultNumber(settings.PostFee)),
		}
		if shopSettings != nil {
			values := shopSettings.All()
			fees["courier"] = feeFromSetting(values[settings.CourierFee], fees["courier"])
			fees["post"] = feeFromSetting(values[settings.PostFee], fees["post"])
		}
		writeJSON(response, http.StatusOK, fees)
	})
}

// feeFromSetting читает цену из настройки. Испорченное значение не должно
// закрыть магазин: витрина покажет то же умолчание, по которому посчитает
// заказ.
func feeFromSetting(raw string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return settings.NonNegative(fallback)
	}
	return settings.NonNegative(value)
}
