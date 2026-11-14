package catalogeditor

import "testing"

func TestWBTNVEDUsesCodeFromOzonDictionaryLabel(t *testing.T) {
	for _, label := range []string{"0601109000", "0601109000 - Прочие цветы: луковицы, клубни", " 0601109000 — Прочие цветы ", "0601109000: Прочие цветы"} {
		values, err := AttributeValues(Attribute{CharcID: 1, Name: "Код ТН ВЭД", Required: true}, []Value{{Value: label, DictionaryID: 971396782}}, "wb")
		if err != nil || len(values) != 1 || values[0].Value != "0601109000" || values[0].DictionaryID != 0 {
			t.Fatalf("invalid WB value: %+v %v", values, err)
		}
	}
	values, err := AttributeValues(Attribute{ID: 2, Name: "Код ТН ВЭД", DictionaryID: 2}, []Value{{Value: "0601109000 - Прочие цветы", DictionaryID: 971396782}}, "ozon")
	if err != nil || values[0].DictionaryID != 971396782 || values[0].Value != "0601109000 - Прочие цветы" {
		t.Fatal("Ozon's selected dictionary value was changed")
	}
}

func TestTNVEDDoesNotInventOrTruncateCodes(t *testing.T) {
	for _, label := range []string{"", "601109000", "06011090001", "Код 0601109000", "0601109000 и 0602909900", "0601109000-", "0601109000\n- другое"} {
		if _, err := TNVEDCode(label); err == nil {
			t.Fatalf("ambiguous or invalid code accepted: %q", label)
		}
	}
}
