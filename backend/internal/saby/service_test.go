package saby

import "testing"

// Код товара в выгрузке зовётся по-разному, а менеджер ищет позицию именно
// по нему. Берём первое непустое и не придумываем ничего лишнего.
// Код товара менеджер видит в СБИС и по нему же ищет позицию. Внутренние
// идентификаторы за код не выдаём: с ними поиск по коду перестал бы
// работать вовсе.
func TestItemCodePrefersHumanNumber(t *testing.T) {
	cases := []struct {
		name string
		item CatalogItem
		want string
	}{
		{"номенклатурный номер", CatalogItem{NomNumber: "X1150532", Code: "17460b69-327e-4ec4-aabb-f064710a135a"}, "X1150532"},
		{"GUID отбрасываем", CatalogItem{Code: "17460b69-327e-4ec4-aabb-f064710a135a"}, ""},
		{"остался артикул", CatalogItem{Article: "ART-1"}, "ART-1"},
		{"пусто", CatalogItem{}, ""},
		{"число вместо строки", CatalogItem{NomNumber: float64(1150532)}, "1150532"},
	}
	for _, item := range cases {
		if got := itemCode(item.item); got != item.want {
			t.Errorf("%s: ожидали %q, получили %q", item.name, item.want, got)
		}
	}
}

func TestLooksLikeGUID(t *testing.T) {
	if !looksLikeGUID("17460b69-327e-4ec4-aabb-f064710a135a") {
		t.Error("настоящий GUID не опознан")
	}
	for _, value := range []string{"X1150532", "", "17460b69327e4ec4aabbf064710a135a", "17460b69-327e-4ec4-aabb-f064710a135z"} {
		if looksLikeGUID(value) {
			t.Errorf("%q принят за GUID", value)
		}
	}
}

func TestNormalizeKeepsCodes(t *testing.T) {
	items := normalizeItems([]CatalogItem{{
		ID: "42", NomNumber: "X1150532", Article: "ART-1", Barcode: "4600000000001",
		Name: "Аглаонема", Cost: 1490.0, Balance: 3.0,
	}})
	if len(items) != 1 {
		t.Fatalf("ожидали одну позицию, получили %d", len(items))
	}
	item := items[0]
	if item.id != "42" {
		t.Errorf("normalization must keep raw id before synchronization: %q", item.id)
	}
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
	})
	if len(items) != 0 {
		t.Fatalf("ожидали пустой список, получили %d", len(items))
	}
}

// Позиция без цены — не мусор: её можно завести в магазин и назначить цену
// самому, поэтому в справочник она обязана попасть.
func TestNormalizeKeepsItemsWithoutPrice(t *testing.T) {
	items := normalizeItems([]CatalogItem{{ID: "7", NomNumber: "X7705223", Name: "Кашпо"}})
	if len(items) != 1 || items[0].costMinor != 0 || items[0].code != "X7705223" {
		t.Fatalf("позиция без цены потерялась: %+v", items)
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

func TestValidateCatalogHealthRejectsTruncatedSnapshot(t *testing.T) {
	items := make([]normalizedItem, 40)
	if err := validateCatalogHealth(items, 100, 0); err == nil {
		t.Fatal("обрезанная выгрузка не должна обнулять отсутствующие товары")
	}
}

func TestValidateCatalogHealthRejectsMassZero(t *testing.T) {
	items := make([]normalizedItem, 100)
	if err := validateCatalogHealth(items, 100, 25); err == nil {
		t.Fatal("полностью нулевая выгрузка не должна затирать рабочие остатки")
	}
}

func TestValidateCatalogHealthAcceptsCompleteSnapshot(t *testing.T) {
	items := make([]normalizedItem, 100)
	items[0].balance = 1
	if err := validateCatalogHealth(items, 100, 25); err != nil {
		t.Fatalf("полная выгрузка отклонена: %v", err)
	}
}
