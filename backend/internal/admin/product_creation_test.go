package admin

import (
	"testing"
)

func validCreationTestInput() CreationInput {
	return CreationInput{Name: "Тестовое растение", LatinName: "Test plant", Description: "Описание для тестового черновика", CategoryID: 1, PriceMinor: 149099, WholesaleMinQty: 1, PotCM: 11.3, HeightCM: 40, Photos: []string{"photo"}, SabyFolder: "STG-CATALOG"}
}
func TestCreationValidationBlocksIncompleteChannelsAndAcceptsExplicitSkips(t *testing.T) {
	wb, ozon := creationSchemas()
	options := CreationOptions{SabyFolders: []CreationFolder{{ID: "STG-CATALOG"}}, WB: wb, Ozon: ozon}
	input := validCreationTestInput()
	if err := validateCreation(input, options); err != nil {
		t.Fatal(err)
	}
	input.WB = CreationChannel{Enabled: true, Category: wb[0].ID, PriceMinor: 100, Values: map[string]string{}}
	if err := validateCreation(input, options); err == nil {
		t.Fatal("accepted enabled marketplace without packaging/attributes")
	}
	input.PackageLengthCM = 20
	input.PackageWidthCM = 20
	input.PackageHeightCM = 40
	input.PackageWeightGrams = 700
	input.WB.Values = map[string]string{"brand": "Тест", "plantKind": "Аглаонема", "country": "Нидерланды", "contents": "Растение в горшке"}
	if err := validateCreation(input, options); err != nil {
		t.Fatal(err)
	}
	input.Ozon = CreationChannel{Enabled: true, Category: ozon[0].ID, PriceMinor: 200, Values: input.WB.Values}
	if err := validateCreation(input, options); err == nil {
		t.Fatal("accepted Ozon with only WB-compatible values")
	}
	input.Ozon.Enabled = false
	if err := validateCreation(input, options); err != nil {
		t.Fatal(err)
	}
	input.Photos = []string{"photo", "photo"}
	if err := validateCreation(input, options); err == nil {
		t.Fatal("accepted duplicate photos")
	}
}
func TestCreationSchemaKeepsPlatformSpecificFieldsSeparate(t *testing.T) {
	wb, ozon := creationSchemas()
	shared := map[string]bool{}
	for _, field := range wb[0].Fields {
		if field.Shared {
			shared[field.Code] = true
		}
	}
	for _, field := range ozon[0].Fields {
		if field.Code == "tnved" || field.Code == "vat" {
			if field.Shared || !field.Required {
				t.Fatalf("platform-specific field can be auto-filled or skipped: %+v", field)
			}
		} else if field.Shared && !shared[field.Code] {
			t.Fatalf("unverified shared field: %+v", field)
		}
	}
}
