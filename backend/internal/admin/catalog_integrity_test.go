package admin

import "testing"

func TestSafeMappingToken(t *testing.T) {
	for _, value := range []string{"wildberries", "offer_id", "partner-2"} {
		if !safeMappingToken(value) { t.Fatalf("valid token rejected: %s",value) }
	}
	for _, value := range []string{"", "WB SKU", "../secret", "озон"} {
		if safeMappingToken(value) { t.Fatalf("unsafe token accepted: %s",value) }
	}
}

func TestValidateAttributeDefinition(t *testing.T) {
	valid := AttributeDefinitionInput{Code: "light_level", Name: "Освещение", DataType: "enum", Audience: "customer", Scope: "product", Options: []AttributeOption{{Code: "bright", Label: "Яркий", Active: true}}}
	if err := validateAttributeDefinition(valid); err != nil { t.Fatalf("valid attribute rejected: %v", err) }

	cases := []AttributeDefinitionInput{
		{Code: "свет", Name: "Свет", DataType: "text", Audience: "customer", Scope: "product"},
		{Code: "care", Name: "Уход", DataType: "enum", Audience: "customer", Scope: "product"},
		{Code: "care", Name: "Уход", DataType: "mystery", Audience: "customer", Scope: "product"},
	}
	for _, input := range cases {
		if err := validateAttributeDefinition(input); err == nil { t.Fatalf("invalid attribute accepted: %+v", input) }
	}
}

func TestValidateCatalogFilter(t *testing.T) {
	if err := validateFilter(CatalogFilterInput{Code: "light-filter", Title: "Освещение", AttributeID: 1, DisplayMode: "chips"}); err != nil { t.Fatalf("valid filter rejected: %v", err) }
	for _, input := range []CatalogFilterInput{
		{Code: "свет", Title: "Освещение", AttributeID: 1, DisplayMode: "chips"},
		{Code: "light", Title: "", AttributeID: 1, DisplayMode: "chips"},
		{Code: "light", Title: "Освещение", AttributeID: 1, DisplayMode: "slider"},
	} {
		if err := validateFilter(input); err == nil { t.Fatalf("invalid filter accepted: %+v", input) }
	}
}

func TestValidateCollectionDefinition(t *testing.T) {
	valid := CollectionDefinitionInput{Slug: "bathroom", Title: "Для ванной", Mode: "dynamic", Rules: []CollectionRule{{Attribute: "placement", Operator: "contains", Value: "bathroom"}}}
	if err := validateCollectionDefinition(&valid); err != nil { t.Fatalf("valid collection rejected: %v", err) }
	invalid := CollectionDefinitionInput{Slug: "bathroom", Title: "Для ванной", Mode: "dynamic"}
	if err := validateCollectionDefinition(&invalid); err == nil { t.Fatal("dynamic collection without rules accepted") }
}
