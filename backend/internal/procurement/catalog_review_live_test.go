package procurement

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCatalogReviewOnlyIncludesUnresolvedActivePurchaseLines(t *testing.T) {
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
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	store := NewPostgresStore(pool)
	unique := time.Now().UnixNano()
	var actorID, supplierID, orderID, planID int64
	must(pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at) VALUES($1,$2,'','Catalog review test',now()) RETURNING id`, fmt.Sprintf("review-%d@example.invalid", unique), fmt.Sprintf("+75%09d", unique%1000000000)).Scan(&actorID))
	must(pool.QueryRow(ctx, `INSERT INTO procurement_suppliers(name,kind,default_currency) VALUES($1,'international','EUR') RETURNING id`, fmt.Sprintf("Review %d", unique)).Scan(&supplierID))
	alias := func(name string) int64 {
		t.Helper()
		var id int64
		must(pool.QueryRow(ctx, `INSERT INTO procurement_supplier_aliases(supplier_id,raw_name,normalized_name,pot_diameter_cm,height_cm,match_status) VALUES($1,$2,$2,15,45,'unmatched') RETURNING id`, supplierID, name).Scan(&id))
		return id
	}
	oldPlanAlias := alias("На штамбе микс")
	must(pool.QueryRow(ctx, `INSERT INTO procurement_orders(supplier_id,source_kind,currency,status,created_by) VALUES($1,'manual','EUR','ordered',$2) RETURNING id`, supplierID, actorID).Scan(&orderID))
	must(pool.QueryRow(ctx, `INSERT INTO procurement_order_lines(procurement_order_id,supplier_alias_id,raw_name,ordered_qty,expected_unit_price,load_unit,pot_diameter_cm,height_cm,match_status,reconciliation_status) VALUES($1,$2,'На штамбе микс',6,16.65,'2',15,45,'unmatched','planned') RETURNING id`, orderID, oldPlanAlias).Scan(&planID))
	pot, height := 15.0, 45.0
	actor := Actor{CustomerID: actorID, Role: "owner"}
	parsed := ParsedDocument{ParserKind: "holland_packing_list", DocumentNumber: fmt.Sprintf("REVIEW-%d", unique), Currency: "EUR", ArithmeticOK: true, ProductSubtotal: 99.9, DocumentTotal: 99.9, CalculatedTotal: 99.9, Lines: []ParsedLine{{SourcePage: 1, SourceLine: 1, RawName: "Citr Mix", Quantity: 6, UnitPrice: 16.65, LineTotal: 99.9, LoadUnit: "2", PotDiameterCM: &pot, HeightCM: &height}}}
	_, err = store.ImportDocument(ctx, actor, DocumentUpload{SupplierID: supplierID, OrderID: orderID, FileName: "review.pdf", ContentType: "application/pdf", Content: []byte(fmt.Sprintf("%%PDF-review-%d", unique))}, parsed)
	must(err)
	var invoiceID, invoiceAliasID int64
	must(pool.QueryRow(ctx, `SELECT id,supplier_alias_id FROM procurement_order_lines WHERE procurement_order_id=$1 AND reconciliation_status='added'`, orderID).Scan(&invoiceID, &invoiceAliasID))
	detail, err := store.UpdateOrderLine(ctx, actor, planID, OrderLineUpdate{InvoiceLineID: &invoiceID})
	must(err)
	if len(detail.Lines) != 1 || detail.Lines[0].ReconciliationStatus != "matched" || detail.Validation.Unmatched != 1 || detail.Validation.CanCalculate {
		t.Fatalf("invoice match must not imply catalog identity: %+v", detail)
	}
	line := detail.Lines[0]
	if line.MatchAlias == nil || line.MatchAlias.ID != invoiceAliasID || line.MatchAlias.RawName != "Citr Mix" || line.MatchAlias.SupplierName != detail.Order.SupplierName {
		t.Fatalf("wrong actionable alias %+v", line.MatchAlias)
	}
	if !strings.Contains(strings.Join(detail.Validation.Blockers, "|"), "Не выбраны товары СБИС: 1") {
		t.Fatalf("ambiguous blocker %v", detail.Validation.Blockers)
	}
	// Historical names and rows remain stored, but must not become work items.
	for _, scenario := range []struct {
		name, status, reconciliation, match string
		excluded                            bool
	}{
		{"received", "received", "matched", "unmatched", false},
		{"cancelled", "cancelled", "planned", "unmatched", false},
		{"superseded", "review", "superseded", "unmatched", false},
		{"excluded", "review", "excluded", "unmatched", true},
		{"confirmed line with stale alias", "review", "matched", "confirmed", false},
	} {
		id := alias(scenario.name)
		var purchaseID int64
		must(pool.QueryRow(ctx, `INSERT INTO procurement_orders(supplier_id,source_kind,currency,status) VALUES($1,'manual','EUR',$2) RETURNING id`, supplierID, scenario.status).Scan(&purchaseID))
		_, err = pool.Exec(ctx, `INSERT INTO procurement_order_lines(procurement_order_id,supplier_alias_id,raw_name,ordered_qty,load_unit,match_status,reconciliation_status,invoice_excluded) VALUES($1,$2,$3,1,'shelf',$4,$5,$6)`, purchaseID, id, scenario.name, scenario.match, scenario.reconciliation, scenario.excluded)
		must(err)
	}
	review, err := store.listReview(ctx)
	must(err)
	count := 0
	for _, item := range review {
		if item.SupplierID == supplierID {
			count++
			if item.ID != invoiceAliasID {
				t.Fatalf("stale alias in queue: %+v", item)
			}
		}
	}
	if count != 1 {
		t.Fatalf("review has %d active aliases, want 1", count)
	}
	dashboard, err := store.Dashboard(ctx)
	must(err)
	var expectedTotal int
	must(pool.QueryRow(ctx, `SELECT COUNT(*) FROM procurement_supplier_aliases a WHERE `+activeAliasReviewPredicate).Scan(&expectedTotal))
	if dashboard.Summary.UnresolvedAliases != expectedTotal {
		t.Fatalf("queue badge %d != %d", dashboard.Summary.UnresolvedAliases, expectedTotal)
	}
	saby := fmt.Sprintf("review-citrus-%d", unique)
	_, err = pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,name,balance,section_path,seen_at) VALUES($1,$1,'Цитрус на штамбе D15',0,ARRAY['Цветы'],now())`, saby)
	must(err)
	_, err = store.ResolveAlias(ctx, actor, invoiceAliasID, AliasResolution{MatchStatus: "confirmed", SabyID: saby})
	must(err)
	detail, err = store.OrderDetail(ctx, orderID)
	must(err)
	if detail.Validation.Unmatched != 0 || detail.Lines[0].SabyID != saby || detail.Lines[0].ReconciliationStatus != "matched" {
		t.Fatalf("catalog match lost invoice facts: %+v", detail)
	}
	review, err = store.listReview(ctx)
	must(err)
	for _, item := range review {
		if item.SupplierID == supplierID {
			t.Fatalf("resolved purchase still in queue: %+v", item)
		}
	}
	var retained int
	must(pool.QueryRow(ctx, `SELECT COUNT(*) FROM procurement_supplier_aliases WHERE supplier_id=$1`, supplierID).Scan(&retained))
	if retained != 7 {
		t.Fatalf("historical aliases were lost: %d", retained)
	}
}
