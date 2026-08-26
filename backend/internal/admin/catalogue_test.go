package admin

import (
	"encoding/json"
	"strings"
	"testing"
)

// Адрес страницы человек читает и пересказывает по телефону, поэтому
// кириллица переводится в латиницу, а не выбрасывается.
func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Фикус Бенджамина":      "fikus-bendzhamina",
		"Аглаонема D12 в асс.":  "aglaonema-d12-v-ass",
		"  Щучий хвост  ":       "schuchiy-hvost",
		"Ficus Elastica":        "ficus-elastica",
		"Кашпо 15 см — белое":   "kashpo-15-sm-beloe",
		"???":                   "",
		"Ель":                   "el",
	}
	for name, want := range cases {
		if got := slugify(name); got != want {
			t.Errorf("%q: ожидали %q, получили %q", name, want, got)
		}
	}
}

// Список кодов менеджер вставляет как придётся: через запятую, с новой
// строки, с лишними пробелами. Разобрать нужно всё это, сохранив порядок.
func TestNormalizeCodes(t *testing.T) {
	got := normalizeCodes([]string{" x1150532, X1150533\nX1150534;X1150532 ", "", "  ", "x1150535\t"})
	want := []string{"X1150532", "X1150533", "X1150534", "X1150535"}
	if len(got) != len(want) {
		t.Fatalf("ожидали %d кодов, получили %d: %v", len(want), len(got), got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Errorf("на месте %d ожидали %s, получили %s", index, want[index], got[index])
		}
	}
}

func TestNormalizeCodesEmpty(t *testing.T) {
	if got := normalizeCodes([]string{"", "  ,  ;"}); len(got) != 0 {
		t.Fatalf("из пустоты получились коды: %v", got)
	}
}

func TestCategoryAttributeContractKeepsStableValueAndRussianLabel(t *testing.T) {
	attribute := CategoryAttribute{Code: "light_level", DataType: "enum", Options: []string{"diffused"}, OptionLabels: map[string]string{"diffused": "Рассеянный свет"}}
	raw, err := json.Marshal(attribute)
	if err != nil { t.Fatal(err) }
	encoded := string(raw)
	if !strings.Contains(encoded, `"options":["diffused"]`) || !strings.Contains(encoded, `"optionLabels":{"diffused":"Рассеянный свет"}`) {
		t.Fatalf("attribute contract lost value or label: %s", encoded)
	}
}
