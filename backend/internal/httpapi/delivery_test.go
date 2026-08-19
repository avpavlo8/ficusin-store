package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/settings"
)

type stubSettingsService map[string]string

func (stub stubSettingsService) All() map[string]string { return stub }

func (stub stubSettingsService) Save(context.Context, map[string]string) error { return nil }

func deliveryFees(t *testing.T, service settingsService) map[string]int {
	t.Helper()
	recorder := httptest.NewRecorder()
	deliveryFeesHandler(service).ServeHTTP(
		recorder, httptest.NewRequest(http.MethodGet, "/api/v1/delivery/fees", nil),
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("витрина получила %d вместо цены доставки", recorder.Code)
	}
	var fees map[string]int
	if err := json.Unmarshal(recorder.Body.Bytes(), &fees); err != nil {
		t.Fatalf("ответ не разобрать: %v", err)
	}
	return fees
}

// Главное свойство: витрина называет ту же цену, что панель. Иначе
// покупатель видит одно число, а счёт получает на другое.
func TestDeliveryFeesFollowThePanel(t *testing.T) {
	fees := deliveryFees(t, stubSettingsService{
		settings.CourierFee: "300",
		settings.PostFee:    "1200",
	})
	if fees["courier"] != 300 {
		t.Fatalf("курьер стоит %d, а в панели 300", fees["courier"])
	}
	if fees["post"] != 1200 {
		t.Fatalf("почта стоит %d, а в панели 1200", fees["post"])
	}
}

func TestFreeDeliveryReachesTheStorefront(t *testing.T) {
	fees := deliveryFees(t, stubSettingsService{settings.CourierFee: "0"})
	if fees["courier"] != 0 {
		t.Fatalf("бесплатную доставку показали за %d ₽", fees["courier"])
	}
}

// Испорченная настройка не должна ломать оформление: показываем то же
// умолчание, по которому посчитает заказ.
func TestBrokenSettingFallsBackToDefault(t *testing.T) {
	fees := deliveryFees(t, stubSettingsService{settings.CourierFee: "бесплатно"})
	if fees["courier"] != 490 {
		t.Fatalf("при испорченной настройке курьер стоит %d, ожидали 490", fees["courier"])
	}
}

func TestNegativeSettingIsNotACredit(t *testing.T) {
	fees := deliveryFees(t, stubSettingsService{settings.PostFee: "-500"})
	if fees["post"] != 0 {
		t.Fatalf("опечатка в панели превратилась в %d ₽", fees["post"])
	}
}

// Магазин без панели вообще (так собирают в тестах) всё равно обязан
// называть настоящую цену.
func TestWithoutPanelStorefrontStillGetsRealPrices(t *testing.T) {
	fees := deliveryFees(t, nil)
	if fees["courier"] != 490 || fees["post"] != 590 {
		t.Fatalf("без панели цены оказались %v", fees)
	}
}
