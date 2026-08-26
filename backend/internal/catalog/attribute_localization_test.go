package catalog

import (
	"reflect"
	"testing"
)

func TestLocalizeAttributeValueFromCanonicalOptions(t *testing.T) {
	tests := []struct {
		name   string
		value  any
		labels map[string]string
		want   any
	}{
		{"pot type", "cachepot", map[string]string{"cachepot": "Кашпо"}, "Кашпо"},
		{"soil type", "ready_mix", map[string]string{"ready_mix": "Готовый грунт"}, "Готовый грунт"},
		{"boolean drainage", true, nil, "Есть"},
		{"multi enum", []any{"root", "foliar"}, map[string]string{"root": "Корневая подкормка", "foliar": "По листу"}, []string{"Корневая подкормка", "По листу"}},
		{"unknown legacy code", "legacy_code", map[string]string{"known": "Известный"}, "legacy_code"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			attribute := ProductAttribute{Value: test.value, OptionLabels: test.labels}
			localizeAttributeValue(&attribute)
			if !reflect.DeepEqual(attribute.DisplayValue, test.want) {
				t.Fatalf("displayValue = %#v, want %#v", attribute.DisplayValue, test.want)
			}
			if !reflect.DeepEqual(attribute.Value, test.value) {
				t.Fatalf("stable value changed: %#v", attribute.Value)
			}
		})
	}
}
