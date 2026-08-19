package order

import (
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/settings"
)

// Заглушка панели: отдаёт ровно то, что в неё положили.
type stubSettings map[string]int

func (stub stubSettings) Number(key string) int { return stub[key] }

func TestDeliveryFeeComesFromPanel(t *testing.T) {
	service := &Service{settings: stubSettings{
		settings.CourierFee: 250,
		settings.PostFee:    700,
	}}
	if got := service.deliveryFee(settings.CourierFee); got != 250 {
		t.Fatalf("курьер стоит %.0f, а в панели 250", got)
	}
	if got := service.deliveryFee(settings.PostFee); got != 700 {
		t.Fatalf("почта стоит %.0f, а в панели 700", got)
	}
}

// Главная ловушка таких настроек: если считать ноль «ничего не задано»,
// владелец никогда не сможет сделать доставку бесплатной: магазин будет
// упрямо возвращать 490 и брать деньги за то, что обещал даром.
func TestZeroMeansFreeDelivery(t *testing.T) {
	service := &Service{settings: stubSettings{settings.CourierFee: 0}}
	if got := service.deliveryFee(settings.CourierFee); got != 0 {
		t.Fatalf("обещали бесплатную доставку, а счёт выставили на %.0f", got)
	}
}

// Отрицательная цена — опечатка в панели. Доставка, уменьшающая счёт,
// магазину не нужна.
func TestNegativeFeeIsNotADiscount(t *testing.T) {
	service := &Service{settings: stubSettings{settings.CourierFee: -1000}}
	if got := service.deliveryFee(settings.CourierFee); got != 0 {
		t.Fatalf("опечатка в панели превратилась в скидку %.0f", got)
	}
}

// Сервис без панели всё равно обязан называть настоящую цену, а не ноль:
// иначе тесты проверяли бы выдуманный магазин с бесплатной доставкой.
func TestWithoutPanelDefaultsApply(t *testing.T) {
	service := &Service{}
	if got := service.deliveryFee(settings.CourierFee); got != 490 {
		t.Fatalf("без панели курьер стоит %.0f, ожидали 490", got)
	}
	if got := service.deliveryFee(settings.PostFee); got != 590 {
		t.Fatalf("без панели почта стоит %.0f, ожидали 590", got)
	}
}

// Умолчания панели и умолчания кода — одно и то же число. Разойдутся —
// цена в панели и цена в счёте перестанут совпадать, и заметит это покупатель.
func TestPanelDefaultsMatchCode(t *testing.T) {
	if settings.DefaultNumber(settings.CourierFee) != 490 {
		t.Fatal("умолчание курьера в панели разошлось с ожидаемым")
	}
	if settings.DefaultNumber(settings.PostFee) != 590 {
		t.Fatal("умолчание почты в панели разошлось с ожидаемым")
	}
}
