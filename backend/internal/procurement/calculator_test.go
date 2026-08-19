package procurement

import (
	"math"
	"testing"
)

func TestPriceChangeThresholdAppliesBothDirections(t *testing.T) {
	if !priceChangeNeeded(1000, 800, .1) || !priceChangeNeeded(1000, 1200, .1) {
		t.Fatal("changes above threshold must be proposed in both directions")
	}
	if priceChangeNeeded(1000, 1050, .1) {
		t.Fatal("small change must be suppressed")
	}
}

func TestAllocatedLineUsesReconciledLogisticsWithoutCurrencyConversion(t *testing.T) {
	settings := PricingSettings{
		RetailMarkupMultiplier: 2,
	}
	result := calculateAllocatedLine(settings, KindInternational, 10, 100, 250, 50, 0)
	if result.PurchaseUnitRUB != 1000 || result.TrolleyDeliveryUnitRUB != 250 || result.RyazanDeliveryUnitRUB != 50 || result.UnitCostRUB != 1300 {
		t.Fatalf("allocated calculation = %#v", result)
	}
	if result.ProposedRetailRUB != 2600 {
		t.Fatalf("retail = %d, want 2600", result.ProposedRetailRUB)
	}
}

// Цена для маркетплейсов не была покрыта ничем, хотя именно она уезжает на
// витрину WB и Ozon. Проверка записывает то, как магазин считает её
// сегодня: упаковка прибавляется к рознице, а проценты возвратов, расходов
// площадки, налога и резерва идут наценкой сверху.
//
// Ставки здесь подобраны так, чтобы их сумма была точной в двоичной
// арифметике: с боевыми 5 + 46 + 8 + 25 процентов произведение попадает
// впритык к целому, и math.Floor может отдать рубль туда или сюда.
func TestMarketplacePriceAddsPackagingAndRates(t *testing.T) {
	settings := PricingSettings{
		RetailMarkupMultiplier:  2,
		PackageRUB:              100,
		ReturnLossRate:          0.25,
		MarketplaceCostRate:     0.5,
		TaxRate:                 0,
		ReserveRate:             0.25,
		MarketplaceStrikeMarkup: 0.5,
	}
	result := calculateAllocatedLine(settings, KindDomestic, 500, 1, 0, 0, 0)

	if result.ProposedRetailRUB != 1000 {
		t.Fatalf("розница = %d, ожидали 1000", result.ProposedRetailRUB)
	}
	// (1000 + 100) × (1 + 0.25 + 0.5 + 0 + 0.25) = 2200
	if result.ProposedMarketplaceRUB != 2200 {
		t.Fatalf("цена маркетплейса = %d, ожидали 2200", result.ProposedMarketplaceRUB)
	}
	// Зачёркнутая цена — надбавка к цене продажи, а не к рознице.
	if result.ProposedMarketplaceStrikeRUB != 3300 {
		t.Fatalf("зачёркнутая цена = %d, ожидали 3300", result.ProposedMarketplaceStrikeRUB)
	}
}

// Слагаемое «логистика площадки» потерялось при переносе формулы из книги
// «04.08.26.xlsx»: там столбец Y считал её как высоту растения, умноженную
// на десять рублей. Магазин из-за этого продавал на WB и Ozon дешевле
// собственного расчёта — на 552 ₽ для тридцатисантиметрового растения.
func TestMarketplacePriceIncludesHeightLogistics(t *testing.T) {
	settings := PricingSettings{
		RetailMarkupMultiplier:    2,
		PackageRUB:                150,
		ReturnLossRate:            0.25,
		MarketplaceCostRate:       0.5,
		ReserveRate:               0.25,
		MarketplaceLogisticsPerCM: 10,
	}
	// Розница 1000, упаковка 150, логистика 30 см × 10 ₽ = 300.
	result := calculateAllocatedLine(settings, KindDomestic, 500, 1, 0, 0, 30)
	if result.ProposedMarketplaceRUB != 2900 {
		t.Fatalf("цена маркетплейса = %d, ожидали 2900", result.ProposedMarketplaceRUB)
	}
	// Розница логистику площадки не видит: в магазине растение отдают в руки.
	if result.ProposedRetailRUB != 1000 {
		t.Fatalf("розница = %d, ожидали 1000", result.ProposedRetailRUB)
	}
}

// У растения без замеров высоты слагаемого просто нет: придумывать за
// поставщика габариты хуже, чем посчитать без них.
func TestUnknownHeightAddsNoLogistics(t *testing.T) {
	settings := PricingSettings{
		RetailMarkupMultiplier:    2,
		PackageRUB:                150,
		ReturnLossRate:            0.25,
		MarketplaceCostRate:       0.5,
		ReserveRate:               0.25,
		MarketplaceLogisticsPerCM: 10,
	}
	result := calculateAllocatedLine(settings, KindDomestic, 500, 1, 0, 0, 0)
	if result.ProposedMarketplaceRUB != 2300 {
		t.Fatalf("цена маркетплейса = %d, ожидали 2300", result.ProposedMarketplaceRUB)
	}
}

// Нулевая ставка выключает слагаемое и возвращает прежнее поведение —
// это способ откатиться, не выкатывая код.
func TestZeroLogisticsRateDisablesTheTerm(t *testing.T) {
	settings := PricingSettings{
		RetailMarkupMultiplier: 2, PackageRUB: 150,
		ReturnLossRate: 0.25, MarketplaceCostRate: 0.5, ReserveRate: 0.25,
	}
	result := calculateAllocatedLine(settings, KindDomestic, 500, 1, 0, 0, 30)
	if result.ProposedMarketplaceRUB != 2300 {
		t.Fatalf("цена маркетплейса = %d, ожидали 2300", result.ProposedMarketplaceRUB)
	}
}

// Упаковка входит в цену маркетплейса и не входит в розницу: в магазине
// растение отдают в руки, на площадке его нужно упаковать и отправить.
func TestPackagingOnlyAffectsMarketplacePrice(t *testing.T) {
	base := PricingSettings{RetailMarkupMultiplier: 2}
	withPackage := base
	withPackage.PackageRUB = 300

	plain := calculateAllocatedLine(base, KindDomestic, 500, 1, 0, 0, 0)
	packed := calculateAllocatedLine(withPackage, KindDomestic, 500, 1, 0, 0, 0)

	if plain.ProposedRetailRUB != packed.ProposedRetailRUB {
		t.Fatalf("упаковка изменила розницу: %d против %d",
			plain.ProposedRetailRUB, packed.ProposedRetailRUB)
	}
	if packed.ProposedMarketplaceRUB-plain.ProposedMarketplaceRUB != 300 {
		t.Fatalf("упаковка добавила к цене маркетплейса %d ₽, ожидали 300",
			packed.ProposedMarketplaceRUB-plain.ProposedMarketplaceRUB)
	}
}

func TestRoundRetailUsesNearestPsychologicalPrice(t *testing.T) {
	tests := []struct {
		value float64
		want  int64
	}{
		{440, 450},
		{980, 990},
		{451, 450},
		{100, 90},
		{120, 150}, // equal distance: prefer the higher price
		{1490, 1490},
	}
	for _, test := range tests {
		if got := roundRetail(test.value, true); got != test.want {
			t.Errorf("roundRetail(%v) = %d, want %d", test.value, got, test.want)
		}
	}
}

// Округление включается настройкой. Выключенное — это обычное округление
// до рубля, а не «как получится».
func TestRoundRetailCanBeSwitchedOff(t *testing.T) {
	if got := roundRetail(1234.4, false); got != 1234 {
		t.Fatalf("без округления цен = %d, ожидали 1234", got)
	}
	if got := roundRetail(1234.6, false); got != 1235 {
		t.Fatalf("без округления цен = %d, ожидали 1235", got)
	}
}

func TestDeliveryIsSplitAcrossParsedTrolleys(t *testing.T) {
	perTrolley := deliveryPerTrolley(160000, 3)
	if math.Abs(perTrolley*3-160000) > 0.000001 {
		t.Fatalf("allocated delivery = %.2f, want 160000", perTrolley*3)
	}
}
