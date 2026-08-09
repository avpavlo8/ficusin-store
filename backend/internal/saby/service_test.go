package saby

import "testing"

// Код товара в выгрузке зовётся по-разному, а менеджер ищет позицию именно
// по нему. Берём первое непустое и не придумываем ничего лишнего.
func TestItemCodeTakesFirstFilled(t *testing.T) {
	cases := []struct {
		name string
		item CatalogItem
		want string
	}{
		{"код", CatalogItem{Code: "X1150532", Article: "ART-1"}, "X1150532"},
		{"внешний номер", CatalogItem{ExternalID: "X1150532"}, "X1150532"},
		{"номенклатурный номер", CatalogItem{NomNumber: "X1150532"}, "X1150532"},
		{"остался артикул", CatalogItem{Article: "ART-1"}, "ART-1"},
		{"пусто", CatalogItem{}, ""},
		{"число вместо строки", CatalogItem{Code: float64(1150532)}, "1150532"},
	}
	for _, item := range cases {
		if got := itemCode(item.item); got != item.want {
			t.Errorf("%s: ожидали %q, получили %q", item.name, item.want, got)
		}
	}
}

func TestNormalizeKeepsCodes(t *testing.T) {
	items := normalizeItems([]CatalogItem{{
		ID: "42", Code: "X1150532", Article: "ART-1", Barcode: "4600000000001",
		Name: "Аглаонема", Cost: 1490.0, Balance: 3.0,
	}})
	if len(items) != 1 {
		t.Fatalf("ожидали одну позицию, получили %d", len(items))
	}
	item := items[0]
	if item.code != "X1150532" || item.article != "ART-1" || item.barcode != "4600000000001" {
		t.Errorf("коды потерялись: %+v", item)
	}
	if item.costMinor != 149000 || item.balance != 3 {
		t.Errorf("цена или остаток посчитаны неверно: %+v", item)
	}
}

// Позиции-папки и снятые с продажи в магазин не попадают — это не товары.
func TestNormalizeSkipsUnsellable(t *testing.T) {
	no := false
	items := normalizeItems([]CatalogItem{
		{ID: "1", Name: "Папка", Cost: 100.0, IsParent: true},
		{ID: "2", Name: "Снят", Cost: 100.0, Published: &no},
		{ID: "3", Name: "", Cost: 100.0},
		{ID: "4", Name: "Без цены"},
	})
	if len(items) != 0 {
		t.Fatalf("ожидали пустой список, получили %d", len(items))
	}
}

func TestSameStrings(t *testing.T) {
	if !sameStrings([]string{"a", "b"}, []string{"a", "b"}) {
		t.Error("одинаковые наборы посчитались разными")
	}
	if sameStrings([]string{"a", "b"}, []string{"b", "a"}) {
		t.Error("порядок фотографий не учтён, а он виден покупателю")
	}
	if sameStrings([]string{"a"}, []string{"a", "b"}) {
		t.Error("разная длина посчиталась одинаковой")
	}
}
