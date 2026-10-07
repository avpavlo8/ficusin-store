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
	store := NewPostgresStore(pool)
	unique := time.Now().UnixNano()
	sabyID := fmt.Sprintf("shared-hibiscus-%d", unique)
	if _, err := pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,name,balance) VALUES($1,$1,'Hibiscus D19',0)`, sabyID); err != nil {
		t.Fatal(err)
	}
	var actorID int64
	if err := pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at) VALUES($1,$2,'','Price test owner',CURRENT_TIMESTAMP) RETURNING id`, fmt.Sprintf("shared-price-%d@example.invalid", unique), fmt.Sprintf("+79%09d", unique%1000000000)).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	var supplierID int64
	if err := pool.QueryRow(ctx, `INSERT INTO procurement_suppliers(name,kind,default_currency) VALUES($1,'international','EUR') RETURNING id`, fmt.Sprintf("Shared price supplier %d", unique)).Scan(&supplierID); err != nil {
		t.Fatal(err)
	}
	actor := Actor{CustomerID: actorID, Role: "owner"}
	plan := func(price string) json.RawMessage {
		return json.RawMessage(fmt.Sprintf(`{"supplierId":%d,"exchangeRate":"100","deliveryToMoscowRub":"0","deliveryToRyazanRub":"0","items":[{"id":"hibiscus","sabyId":%q,"sabyName":"Hibiscus D19","article":"H19","category":"Hibiscus","recommendedQty":0,"packageCount":"1","unitsPerPackage":"1","expectedUnitPrice":%q,"potDiameterCm":"19","heightCm":"60","loadUnit":"shelf"}]}`, supplierID, sabyID, price))
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
	var orderID int64
	if err := pool.QueryRow(ctx, `INSERT INTO procurement_orders(supplier_id,source_kind,currency,status,created_by) VALUES($1,'recommendation','EUR','ordered',$2) RETURNING id`, supplierID, actorID).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO procurement_order_lines(procurement_order_id,saby_id,raw_name,supplier_article,ordered_qty,expected_unit_price,load_unit,pot_diameter_cm,height_cm,match_status,reconciliation_status) VALUES($1,$2,'Hibiscus D19','H19',1,2,'shelf',19,60,'confirmed','planned')`, orderID, sabyID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO procurement_supplier_aliases(supplier_id,raw_name,normalized_name,supplier_article,pot_diameter_cm,height_cm,matched_saby_id,match_status) VALUES($1,'Hibiscus invoice','hibiscus invoice','H19',19,60,$2,'confirmed')`, supplierID, sabyID); err != nil {
		t.Fatal(err)
	}
	pot, height := 19.0, 60.0
	parsed := ParsedDocument{ParserKind: "holland_packing_list", DocumentNumber: fmt.Sprintf("SHARED-%d", unique), Currency: "EUR", ArithmeticOK: true, ProductSubtotal: 3, DocumentTotal: 3, CalculatedTotal: 3, Lines: []ParsedLine{{SourcePage: 1, SourceLine: 1, RawName: "Hibiscus invoice", SupplierArticle: "H19", PotDiameterCM: &pot, HeightCM: &height, Quantity: 1, UnitPrice: 3, LineTotal: 3, LoadUnit: "shelf"}}}
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
