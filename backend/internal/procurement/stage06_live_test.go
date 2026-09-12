package procurement

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStage06PlanAndInvoiceRevisionsOnLiveDatabase(t *testing.T) {
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
	ficusID := fmt.Sprintf("stage06-ficus-%d", unique)
	flyID := fmt.Sprintf("stage06-fly-%d", unique)
	if _, err = pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,name,balance) VALUES
		($1,$1,'Stage 06 ficus',0),($2,$2,'Stage 06 flytrap',0)`, ficusID, flyID); err != nil {
		t.Fatal(err)
	}
	var actorID int64
	if err = pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at)
		VALUES($1,$2,'','Stage 06 owner',CURRENT_TIMESTAMP) RETURNING id`, fmt.Sprintf("stage06-%d@example.invalid", unique), fmt.Sprintf("+79%09d", unique%1000000000)).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	var supplierID int64
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_suppliers(name,kind,default_currency) VALUES($1,'international','EUR') RETURNING id`, fmt.Sprintf("Stage 06 %d", unique)).Scan(&supplierID); err != nil {
		t.Fatal(err)
	}
	var orderID int64
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_orders(supplier_id,order_number,source_kind,currency,status,created_by)
		VALUES($1,'STAGE-06','recommendation','EUR','ordered',$2) RETURNING id`, supplierID, actorID).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	var ficusLineID, flyLineID int64
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_order_lines(procurement_order_id,saby_id,raw_name,supplier_article,
		ordered_qty,expected_unit_price,load_unit,pot_diameter_cm,height_cm,match_status,package_count,units_per_package,supplier_category)
		VALUES($1,$2,'Ficus plan','PLAN-KEEP',12,5.30,'1',12,35,'confirmed',1,12,'Ficus') RETURNING id`, orderID, ficusID).Scan(&ficusLineID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_order_lines(procurement_order_id,saby_id,raw_name,supplier_article,
		ordered_qty,expected_unit_price,load_unit,pot_diameter_cm,height_cm,match_status,package_count,units_per_package,supplier_category)
		VALUES($1,$2,'Venus flytrap','FLY-1',6,4.20,'1',9,15,'confirmed',1,6,'Carnivorous') RETURNING id`, orderID, flyID).Scan(&flyLineID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO procurement_supplier_aliases(supplier_id,raw_name,normalized_name,supplier_article,
		matched_saby_id,match_status,pot_diameter_cm,height_cm) VALUES($1,'Ficus invoice','ficus invoice','',$2,'confirmed',12,35)`, supplierID, ficusID); err != nil {
		t.Fatal(err)
	}

	first := ParsedDocument{ParserKind: "holland_packing_list", DocumentNumber: "INV-06", Currency: "EUR", ArithmeticOK: true,
		ProductSubtotal: 68.90, DocumentTotal: 68.90, CalculatedTotal: 68.90, Lines: []ParsedLine{
			{SourcePage: 1, SourceLine: 1, RawName: "Ficus invoice", Quantity: 12, UnitPrice: 5.30, LineTotal: 63.60, LoadUnit: "2"},
			{SourcePage: 1, SourceLine: 2, RawName: "New plant", SupplierArticle: "NEW-1", Quantity: 1, UnitPrice: 5.30, LineTotal: 5.30, LoadUnit: "2"},
		}}
	result, err := store.ImportDocument(ctx, Actor{CustomerID: actorID, Role: "owner"}, DocumentUpload{SupplierID: supplierID, OrderID: orderID, FileName: "invoice-v1.pdf", ContentType: "application/pdf", Content: []byte("%PDF-stage06-v1")}, first)
	if err != nil {
		t.Fatal(err)
	}
	if result.Order.ID != orderID {
		t.Fatalf("invoice attached to order %d, want %d", result.Order.ID, orderID)
	}
	var article, category, status string
	var packages, multiple int
	var price float64
	if err = pool.QueryRow(ctx, `SELECT supplier_article,supplier_category,package_count,units_per_package,expected_unit_price::DOUBLE PRECISION,reconciliation_status
		FROM procurement_order_lines WHERE id=$1`, ficusLineID).Scan(&article, &category, &packages, &multiple, &price, &status); err != nil {
		t.Fatal(err)
	}
	if article != "PLAN-KEEP" || category != "Ficus" || packages != 1 || multiple != 12 || price != 5.30 || status != "matched" {
		t.Fatalf("plan was overwritten: article=%q category=%q pack=%dx%d price=%.2f status=%s", article, category, packages, multiple, price, status)
	}
	if err = pool.QueryRow(ctx, `SELECT reconciliation_status FROM procurement_order_lines WHERE id=$1`, flyLineID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "missing" {
		t.Fatalf("missing line status=%s", status)
	}
	if duplicate, err := store.ImportDocument(ctx, Actor{CustomerID: actorID}, DocumentUpload{SupplierID: supplierID, OrderID: orderID, FileName: "same.pdf", ContentType: "application/pdf", Content: []byte("%PDF-stage06-v1")}, first); err != nil || !duplicate.Duplicate {
		t.Fatalf("repeat duplicate=%v err=%v", duplicate.Duplicate, err)
	}

	changed := first
	changed.ProductSubtotal, changed.DocumentTotal, changed.CalculatedTotal = 60.30, 60.30, 60.30
	changed.Lines = append([]ParsedLine(nil), first.Lines...)
	changed.Lines[0].Quantity, changed.Lines[0].UnitPrice, changed.Lines[0].LineTotal = 10, 5.50, 55
	if _, err = store.ImportDocument(ctx, Actor{CustomerID: actorID, Role: "owner"}, DocumentUpload{SupplierID: supplierID, OrderID: orderID, FileName: "invoice-v2.pdf", ContentType: "application/pdf", Content: []byte("%PDF-stage06-v2")}, changed); err != nil {
		t.Fatal(err)
	}
	var activeDocuments, visibleLines, history int
	if err = pool.QueryRow(ctx, `SELECT COUNT(*) FILTER(WHERE superseded_at IS NULL)::INTEGER,COUNT(*) FILTER(WHERE superseded_at IS NOT NULL)::INTEGER FROM procurement_documents WHERE procurement_order_id=$1`, orderID).Scan(&activeDocuments, &history); err != nil {
		t.Fatal(err)
	}
	if activeDocuments != 1 || history != 1 {
		t.Fatalf("document revisions active=%d old=%d", activeDocuments, history)
	}
	if err = pool.QueryRow(ctx, `SELECT COUNT(*)::INTEGER FROM procurement_order_lines WHERE procurement_order_id=$1 AND reconciliation_status<>'superseded'`, orderID).Scan(&visibleLines); err != nil {
		t.Fatal(err)
	}
	if visibleLines != 3 {
		t.Fatalf("changed invoice duplicated active lines: %d", visibleLines)
	}
	if err = pool.QueryRow(ctx, `SELECT reconciliation_status FROM procurement_order_lines WHERE id=$1`, ficusLineID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "changed" {
		t.Fatalf("changed line status=%s", status)
	}
	if err = pool.QueryRow(ctx, `SELECT COUNT(*)::INTEGER FROM procurement_invoice_line_history WHERE procurement_order_line_id=$1`, ficusLineID).Scan(&history); err != nil || history != 1 {
		t.Fatalf("line history=%d err=%v", history, err)
	}

	excluded := true
	reason := "Поставщик не включил позицию"
	if _, err = store.UpdateOrderLine(ctx, Actor{CustomerID: actorID, Role: "owner"}, flyLineID, OrderLineUpdate{InvoiceExcluded: &excluded, ExclusionReason: &reason}); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT reconciliation_status,invoice_exclusion_reason FROM procurement_order_lines WHERE id=$1`, flyLineID).Scan(&status, &article); err != nil {
		t.Fatal(err)
	}
	if status != "excluded" || article != reason {
		t.Fatalf("exclusion status=%s reason=%q", status, article)
	}
	if _, err = store.UpdateOrderStatus(ctx, Actor{CustomerID: actorID, Role: "owner"}, orderID, OrderStatusUpdate{Status: "cancelled", Note: "Stage 06 test"}); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT COUNT(*)::INTEGER FROM procurement_documents WHERE procurement_order_id=$1`, orderID).Scan(&history); err != nil || history != 2 {
		t.Fatalf("cancel lost document history=%d err=%v", history, err)
	}
}
