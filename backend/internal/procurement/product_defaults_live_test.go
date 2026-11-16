package procurement

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStage06SharedPlanPriceAndInvoicePriceOnLiveDatabase(t *testing.T) {
	dsn := os.Getenv("CRM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("CRM_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := NewPostgresStore(pool).WithFileStorage(newPrivateTestFileStorage())
	unique := time.Now().UnixNano()
	sabyID := fmt.Sprintf("shared-hibiscus-%d", unique)
	if _, err := pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,name,balance) VALUES($1,$1,'Hibiscus D19',0)`, sabyID); err != nil {
		t.Fatal(err)
	}
	var actorID int64
	if err := pool.QueryRow(ctx, `INSERT INTO customers(email,phone,full_name,consent_at) VALUES($1,$2,'Price test owner',CURRENT_TIMESTAMP) RETURNING id`, fmt.Sprintf("shared-price-%d@example.invalid", unique), fmt.Sprintf("+79%09d", unique%1000000000)).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	var supplierID int64
	if err := pool.QueryRow(ctx, `INSERT INTO procurement_suppliers(name,kind,default_currency) VALUES($1,'international','EUR') RETURNING id`, fmt.Sprintf("Shared price supplier %d", unique)).Scan(&supplierID); err != nil {
		t.Fatal(err)
	}
	actor := Actor{CustomerID: actorID, Role: "owner"}
	plan := func(price string) json.RawMessage {
		return json.RawMessage(fmt.Sprintf(`{"supplierId":%d,"exchangeRate":"100","deliveryToMoscowRub":"0","deliveryToRyazanRub":"0","items":[{"id":"hibiscus","sabyId":%q,"sabyName":"Hibiscus D19","article":"H19","category":"Hibiscus","recommendedQty":0,"packageCount":"1","unitsPerPackage":"6","expectedUnitPrice":%q,"potDiameterCm":"19","heightCm":"60","loadUnit":"shelf"}]}`, supplierID, sabyID, price))
	}
	first, err := store.CreateNamedPlanDraft(ctx, actor, "First", plan("1"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateNamedPlanDraft(ctx, actor, "Second", plan("1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateNamedPlanDraft(ctx, actor, first.ID, "First", plan("2")); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadNamedPlanDraft(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	var openPlan planDocument
	if err := json.Unmarshal(loaded.Payload, &openPlan); err != nil {
		t.Fatal(err)
	}
	if len(openPlan.Items) != 1 || openPlan.Items[0].ExpectedUnitPrice != "2.0000" {
		t.Fatalf("other plan did not read current supplier price: %+v", openPlan.Items)
	}
	balances, err := store.ListNomenclatureBalances(ctx, supplierID, []string{sabyID})
	if err != nil {
		t.Fatal(err)
	}
	if len(balances) != 1 || len(balances[0].Defaults) != 1 {
		t.Fatalf("product defaults not available for a new purchase: %+v", balances)
	}
	defaults := balances[0].Defaults[0]
	if defaults.PotDiameterCM == nil || *defaults.PotDiameterCM != 19 || defaults.HeightCM == nil || *defaults.HeightCM != 60 || defaults.Category != "Hibiscus" || defaults.Article != "H19" || defaults.UnitPrice == nil || *defaults.UnitPrice != 2 || defaults.UnitsPerPackage != 6 {
		t.Fatalf("supplier product fields did not round-trip: %+v", defaults)
	}
	pot, height := 19.0, 60.0
	order, err := store.CreatePlan(ctx, actor, PlanCreate{SupplierID: supplierID, Costs: &CalculationInput{ExchangeRate: 100}, Items: []PlanItem{{SabyID: sabyID, RawName: "Hibiscus D19", Category: "Hibiscus", SupplierArticle: "H19", Quantity: 6, PackageCount: 1, UnitsPerPackage: 6, ExpectedUnitPrice: 2, PotDiameterCM: &pot, HeightCM: &height, LoadUnit: "shelf"}}})
	if err != nil {
		t.Fatal(err)
	}
	orderID := order.ID
	var savedPot, savedHeight, savedPrice float64
	var savedCategory, savedArticle string
	var savedPackSize int
	if err := pool.QueryRow(ctx, `SELECT pot_diameter_cm::DOUBLE PRECISION,height_cm::DOUBLE PRECISION,expected_unit_price::DOUBLE PRECISION,supplier_category,supplier_article,units_per_package FROM procurement_order_lines WHERE procurement_order_id=$1`, orderID).Scan(&savedPot, &savedHeight, &savedPrice, &savedCategory, &savedArticle, &savedPackSize); err != nil {
		t.Fatal(err)
	}
	if savedPot != 19 || savedHeight != 60 || savedPrice != 2 || savedCategory != "Hibiscus" || savedArticle != "H19" || savedPackSize != 6 {
		t.Fatalf("fixed purchase lost product fields: pot=%v height=%v price=%v name=%q article=%q pack=%v", savedPot, savedHeight, savedPrice, savedCategory, savedArticle, savedPackSize)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO procurement_supplier_aliases(supplier_id,raw_name,normalized_name,supplier_article,pot_diameter_cm,height_cm,matched_saby_id,match_status) VALUES($1,'Hibiscus invoice','hibiscus invoice','H19',19,60,$2,'confirmed')`, supplierID, sabyID); err != nil {
		t.Fatal(err)
	}
	parsed := ParsedDocument{ParserKind: "holland_packing_list", DocumentNumber: fmt.Sprintf("SHARED-%d", unique), Currency: "EUR", ArithmeticOK: true, ProductSubtotal: 18, DocumentTotal: 18, CalculatedTotal: 18, Lines: []ParsedLine{{SourcePage: 1, SourceLine: 1, RawName: "Hibiscus invoice", SupplierArticle: "H19", PotDiameterCM: &pot, HeightCM: &height, Quantity: 6, UnitPrice: 3, LineTotal: 18, LoadUnit: "shelf"}}}
	if _, err := store.ImportDocument(ctx, actor, DocumentUpload{SupplierID: supplierID, OrderID: orderID, FileName: "hibiscus.pdf", ContentType: "application/pdf", Content: []byte(fmt.Sprintf("%%PDF-shared-%d", unique))}, parsed); err != nil {
		t.Fatal(err)
	}
	var expected, actual float64
	if err := pool.QueryRow(ctx, `SELECT expected_unit_price::DOUBLE PRECISION,unit_price::DOUBLE PRECISION FROM procurement_order_lines WHERE procurement_order_id=$1`, orderID).Scan(&expected, &actual); err != nil {
		t.Fatal(err)
	}
	if expected != 2 || actual != 3 {
		t.Fatalf("order price history changed: planned %v, invoice %v", expected, actual)
	}
	var invoiceName string
	if err := pool.QueryRow(ctx, `SELECT invoice_raw_name FROM procurement_order_lines WHERE procurement_order_id=$1`, orderID).Scan(&invoiceName); err != nil {
		t.Fatal(err)
	}
	if invoiceName != "Hibiscus invoice" {
		t.Fatalf("original invoice name was not preserved: %q", invoiceName)
	}
	loaded, err = store.LoadNamedPlanDraft(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(loaded.Payload, &openPlan); err != nil {
		t.Fatal(err)
	}
	if openPlan.Items[0].ExpectedUnitPrice != "3.0000" {
		t.Fatalf("invoice price did not reach open plan: %q", openPlan.Items[0].ExpectedUnitPrice)
	}
}
