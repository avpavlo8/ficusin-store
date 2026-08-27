package catalog

import (
	"encoding/json"
	"testing"
)

// Public attributes keep stable machine values for filters and links while
// carrying the customer-facing label separately. Internal schema metadata must
// not leak into the storefront contract.
func TestProductAttributePublicJSONContract(t *testing.T) {
	attribute := ProductAttribute{
		Code:         "material",
		Name:         "Материал",
		Value:        "ceramic",
		DisplayValue: "Керамика",
		Options:      []string{"ceramic"},
		OptionLabels: map[string]string{"ceramic": "Керамика"},
		DataType:     "enum",
		Filterable:   true,
	}

	raw, err := json.Marshal(attribute)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["value"] != "ceramic" {
		t.Fatalf("machine value changed in public contract: %#v", payload["value"])
	}
	if payload["displayValue"] != "Керамика" {
		t.Fatalf("display label missing from public contract: %#v", payload["displayValue"])
	}
	labels, ok := payload["optionLabels"].(map[string]any)
	if !ok || labels["ceramic"] != "Керамика" {
		t.Fatalf("option labels missing from public contract: %#v", payload["optionLabels"])
	}
	if _, leaked := payload["dataType"]; leaked {
		t.Fatalf("internal dataType leaked into public contract: %s", raw)
	}
}
