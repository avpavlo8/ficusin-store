package procurement

import (
	"context"
	"testing"
)

func TestPlanAllowsMultipleNewProductsAndCalculatesPackageQuantity(t *testing.T) {
	store := &storeStub{}
	_, err := NewService(store).CreatePlan(context.Background(), Actor{}, PlanCreate{SupplierID: 1, Items: []PlanItem{
		{RawName: " Olea ", PackageCount: 2, UnitsPerPackage: 12, Quantity: 999, ExpectedUnitPrice: 10},
		{RawName: "Citrus", PackageCount: 1, UnitsPerPackage: 6, ExpectedUnitPrice: 5},
	}})
	if err != nil { t.Fatal(err) }
	if store.planInput.Items[0].Quantity != 24 || store.planInput.Items[0].RawName != "Olea" { t.Fatalf("wrong normalized plan: %+v", store.planInput) }
}

func TestPlanPreservesSupplierFieldsNeededForNextOrder(t *testing.T) {
	store := &storeStub{}
	pot, height := 12.0, 35.0
	_, err := NewService(store).CreatePlan(context.Background(), Actor{}, PlanCreate{SupplierID: 1, Items: []PlanItem{{
		SabyID: "saby-1", RawName: "Цитрус", Category: " Цитрус ", SupplierArticle: " NL-42 ",
		PackageCount: 2, UnitsPerPackage: 12, ExpectedUnitPrice: 5.3,
		PotDiameterCM: &pot, HeightCM: &height,
	}}})
	if err != nil { t.Fatal(err) }
	item := store.planInput.Items[0]
	if item.Category != "Цитрус" || item.SupplierArticle != "NL-42" || item.PackageCount != 2 ||
		item.UnitsPerPackage != 12 || item.Quantity != 24 || item.ExpectedUnitPrice != 5.3 ||
		item.PotDiameterCM == nil || *item.PotDiameterCM != 12 || item.HeightCM == nil || *item.HeightCM != 35 {
		t.Fatalf("supplier fields were not preserved: %+v", item)
	}
}

func TestPlanRejectsPartialPackagingAndDuplicateLinkedProducts(t *testing.T) {
	for _, items := range [][]PlanItem{
		{{RawName: "Olea", PackageCount: 2, Quantity: 12}},
		{{SabyID: "X1", Quantity: 1}, {SabyID: "X1", Quantity: 1}},
		{{RawName: "Olea", PackageCount: 1000000, UnitsPerPackage: 1000000}},
		{{Quantity: 1}},
	} {
		if _, err := NewService(&storeStub{}).CreatePlan(context.Background(), Actor{}, PlanCreate{SupplierID: 1, Items: items}); err == nil { t.Fatalf("accepted invalid items: %+v", items) }
	}
}
