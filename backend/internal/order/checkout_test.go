package order

import (
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
)

// mergeItems стоит между корзиной и складом. Если одно и то же растение
// приедет двумя строками, каждая проверится против остатка отдельно —
// магазин пообещает две штуки там, где на полке одна.
func TestMergeItemsSumsTheSamePlant(t *testing.T) {
	merged := mergeItems([]ItemInput{
		{ID: "ficus-lyrata", Quantity: 1},
		{ID: "monstera", Quantity: 2},
		{ID: "ficus-lyrata", Quantity: 3},
	})
	if len(merged) != 2 {
		t.Fatalf("строк получилось %d, ожидали 2: %v", len(merged), merged)
	}
	if merged[0].ID != "ficus-lyrata" || merged[0].Quantity != 4 {
		t.Fatalf("фикус слился неверно: %v", merged[0])
	}
	if merged[1].ID != "monstera" || merged[1].Quantity != 2 {
		t.Fatalf("монстера пострадала при слиянии: %v", merged[1])
	}
}

// Порядок строк — это порядок в письме покупателю и в заказе у менеджера.
// Он должен совпадать с тем, как товары попали в корзину.
func TestMergeItemsKeepsFirstAppearanceOrder(t *testing.T) {
	merged := mergeItems([]ItemInput{
		{ID: "c", Quantity: 1},
		{ID: "a", Quantity: 1},
		{ID: "b", Quantity: 1},
		{ID: "a", Quantity: 1},
	})
	order := make([]string, 0, len(merged))
	for _, item := range merged {
		order = append(order, item.ID)
	}
	if len(order) != 3 || order[0] != "c" || order[1] != "a" || order[2] != "b" {
		t.Fatalf("порядок строк изменился: %v", order)
	}
}

// Пробелы приходят из браузера и не делают растение другим растением.
// Иначе « ficus » и «ficus» проверятся против остатка по отдельности.
func TestMergeItemsTreatsPaddedIDAsTheSamePlant(t *testing.T) {
	merged := mergeItems([]ItemInput{
		{ID: " ficus ", Quantity: 1},
		{ID: "ficus", Quantity: 2},
	})
	if len(merged) != 1 {
		t.Fatalf("одно растение разъехалось на %d строки: %v", len(merged), merged)
	}
	if merged[0].ID != "ficus" || merged[0].Quantity != 3 {
		t.Fatalf("слияние по обрезанному коду не сработало: %v", merged[0])
	}
}

func TestMergeItemsDropsEmptyIDs(t *testing.T) {
	merged := mergeItems([]ItemInput{
		{ID: "", Quantity: 5},
		{ID: "   ", Quantity: 5},
		{ID: "ficus", Quantity: 1},
	})
	if len(merged) != 1 || merged[0].ID != "ficus" {
		t.Fatalf("мусор из браузера доехал до заказа: %v", merged)
	}
}

// Три одинаковых растения едут тремя коробками рядом, а не одной. Если
// считать их за одну, СДЭК назовёт цену за треть посылки.
func TestShippingBoxCountsEveryCopy(t *testing.T) {
	item := purchasableItem{
		Quantity: 3,
		Parcel: integration.Parcel{
			LengthCM: 20, WidthCM: 20, HeightCM: 30, WeightGrams: 1000,
		},
	}
	box, measured := shippingBox([]purchasableItem{item})
	if !measured {
		t.Fatal("посылка из измеренных растений осталась неизмеренной")
	}
	if box.WeightGrams != 3000 {
		t.Fatalf("вес посылки %d г, ожидали 3000", box.WeightGrams)
	}
	// Коробки ставятся рядом: длинная сторона общая, ширины складываются.
	if box.LengthCM != 30 || box.WidthCM != 60 || box.HeightCM != 20 {
		t.Fatalf("габариты посылки %dx%dx%d", box.LengthCM, box.WidthCM, box.HeightCM)
	}
}

// Одно неизмеренное растение — и цену доставки называет менеджер, а не мы.
// Придумать габариты за поставщика хуже, чем признать, что их нет:
// покупатель заплатит за посылку, которая не поедет.
func TestShippingBoxRefusesWhenSomethingIsUnmeasured(t *testing.T) {
	items := []purchasableItem{
		{Quantity: 1, Parcel: integration.Parcel{
			LengthCM: 20, WidthCM: 20, HeightCM: 30, WeightGrams: 1000,
		}},
		{Quantity: 1, Parcel: integration.Parcel{}},
	}
	if _, measured := shippingBox(items); measured {
		t.Fatal("неизмеренное растение получило посчитанную цену доставки")
	}
}

func TestShippingBoxRefusesEmptyCart(t *testing.T) {
	if _, measured := shippingBox(nil); measured {
		t.Fatal("пустая корзина получила посылку")
	}
}
