package admin

import "testing"

func TestBuildMergeSuggestionsOnlyGroupsExactPlantNameWithoutPotSize(t *testing.T) {
	category := int64(7)
	result := BuildMergeSuggestions([]Product{
		{ID: 1, Name: "Фикус Бенджамина Кинки D12", Status: "draft", CatalogSection: "plants", CategoryID: &category, Stock: 3},
		{ID: 2, Name: "Фикус Бенджамина Кинки D 17", Status: "draft", CatalogSection: "plants", CategoryID: &category, Stock: 2, Description: "Заполнено"},
		{ID: 3, Name: "Фикус Бенджамина Грин Кинки D12", Status: "draft", CatalogSection: "plants", CategoryID: &category, Stock: 1},
		{ID: 4, Name: "Фикус Бенджамина Кинки D10", Status: "published", CatalogSection: "plants", CategoryID: &category},
		{ID: 5, Name: "Кашпо D12", Status: "draft", CatalogSection: "pots", CategoryID: &category},
	})
	if len(result.Suggestions) != 1 {
		t.Fatalf("groups=%d want=1", len(result.Suggestions))
	}
	group := result.Suggestions[0]
	if len(group.Products) != 2 || group.Products[0].ID != 2 {
		t.Fatalf("unexpected group: %+v", group.Products)
	}
	if result.Audit.Drafts != 4 || result.Audit.CardsInGroups != 2 {
		t.Fatalf("audit=%+v", result.Audit)
	}
}

func TestBuildMergeSuggestionsDoesNotCrossCategories(t *testing.T) {
	left, right := int64(7), int64(8)
	result := BuildMergeSuggestions([]Product{
		{ID: 1, Name: "Аглаонема Мария D12", Status: "draft", CatalogSection: "plants", CategoryID: &left},
		{ID: 2, Name: "Аглаонема Мария D17", Status: "draft", CatalogSection: "plants", CategoryID: &right},
	})
	if len(result.Suggestions) != 0 {
		t.Fatalf("cross-category group created: %+v", result.Suggestions)
	}
}

func TestPotDiameterFromName(t *testing.T) {
	for _, test := range []struct{name string; want int; ok bool}{
		{name: "Фикус D12", want: 12, ok: true},
		{name: "Монстера D 17", want: 17, ok: true},
		{name: "Хойя Ø6", want: 6, ok: true},
		{name: "Удобрение 12", ok: false},
	} {
		got, ok := potDiameterFromName(test.name)
		if got != test.want || ok != test.ok { t.Fatalf("%q: got=(%d,%v), want=(%d,%v)", test.name, got, ok, test.want, test.ok) }
	}
}
