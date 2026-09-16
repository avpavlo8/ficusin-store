package procurement

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestResolveAliasCollapsesLateInvoiceLineOnLiveDatabase(t *testing.T) {
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
	sabyID := fmt.Sprintf("late-fittonia-%d", unique)
	if _, err = pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,name,balance,section_path,seen_at)
		VALUES($1,$1,'Фиттония D9',0,ARRAY['Цветы'],CURRENT_TIMESTAMP)`, sabyID); err != nil {
		t.Fatal(err)
	}
	var actorID int64
	if err = pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at)
		VALUES($1,$2,'','Late alias owner',CURRENT_TIMESTAMP) RETURNING id`, fmt.Sprintf("late-alias-%d@example.invalid", unique), fmt.Sprintf("+78%09d", unique%1000000000)).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	var supplierID int64
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_suppliers(name,kind,country_code,default_currency)
		VALUES($1,'international','NL','EUR') RETURNING id`, fmt.Sprintf("Late alias supplier %d", unique)).Scan(&supplierID); err != nil {
		t.Fatal(err)
	}
	var orderID, plannedID int64
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_orders(supplier_id,order_number,source_kind,currency,status,created_by)
		VALUES($1,$2,'recommendation','EUR','ordered',$3) RETURNING id`, supplierID, fmt.Sprintf("LATE-%d", unique), actorID).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_order_lines(procurement_order_id,saby_id,raw_name,ordered_qty,
		expected_unit_price,load_unit,pot_diameter_cm,height_cm,match_status,reconciliation_status)
		VALUES($1,$2,'Фиттония D9',21,4.00,'shelf',8.5,12.5,'confirmed','planned') RETURNING id`, orderID, sabyID).Scan(&plannedID); err != nil {
		t.Fatal(err)
	}

	parsed := ParsedDocument{ParserKind: "holland_packing_list", DocumentNumber: fmt.Sprintf("INV-LATE-%d", unique), Currency: "EUR", ArithmeticOK: true,
		ProductSubtotal: 84, DocumentTotal: 84, CalculatedTotal: 84, Lines: []ParsedLine{{SourcePage: 1, SourceLine: 1, RawName: "Fitt Gem 3 Kl", Quantity: 21, UnitPrice: 4, LineTotal: 84, LoadUnit: "shelf"}}}
	if _, err = store.ImportDocument(ctx, Actor{CustomerID: actorID, Role: "owner"}, DocumentUpload{SupplierID: supplierID, OrderID: orderID,
		FileName: "fittonia.pdf", ContentType: "application/pdf", Content: []byte(fmt.Sprintf("%%PDF-late-alias-%d", unique))}, parsed); err != nil {
		t.Fatal(err)
	}

	var aliasID, addedID int64
	if err = pool.QueryRow(ctx, `SELECT id FROM procurement_supplier_aliases WHERE supplier_id=$1 AND raw_name='Fitt Gem 3 Kl' ORDER BY id DESC LIMIT 1`, supplierID).Scan(&aliasID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT id FROM procurement_order_lines WHERE procurement_order_id=$1 AND reconciliation_status='added'`, orderID).Scan(&addedID); err != nil {
		t.Fatal(err)
	}
	var status string
	if err = pool.QueryRow(ctx, `SELECT reconciliation_status FROM procurement_order_lines WHERE id=$1`, plannedID).Scan(&status); err != nil || status != "missing" {
		t.Fatalf("planned line before mapping status=%q err=%v", status, err)
	}

	if _, err = store.ResolveAlias(ctx, Actor{CustomerID: actorID, Role: "owner"}, aliasID, AliasResolution{MatchStatus: "confirmed", SabyID: sabyID}); err != nil {
		t.Fatal(err)
	}

	var activeLines, invoiced int
	var invoiceName string
	var hasDocument bool
	if err = pool.QueryRow(ctx, `SELECT COUNT(*)::INTEGER FROM procurement_order_lines WHERE procurement_order_id=$1 AND reconciliation_status<>'superseded'`, orderID).Scan(&activeLines); err != nil {
		t.Fatal(err)
	}
	if activeLines != 1 {
		t.Fatalf("visible lines=%d, want 1", activeLines)
	}
	if err = pool.QueryRow(ctx, `SELECT reconciliation_status,procurement_document_id IS NOT NULL,invoice_raw_name,invoiced_qty
		FROM procurement_order_lines WHERE id=$1`, plannedID).Scan(&status, &hasDocument, &invoiceName, &invoiced); err != nil {
		t.Fatal(err)
	}
	if status != "matched" || !hasDocument || invoiceName != "Fitt Gem 3 Kl" || invoiced != 21 {
		t.Fatalf("collapsed plan status=%q document=%v invoice=%q qty=%d", status, hasDocument, invoiceName, invoiced)
	}
	if err = pool.QueryRow(ctx, `SELECT reconciliation_status FROM procurement_order_lines WHERE id=$1`, addedID).Scan(&status); err != nil || status != "superseded" {
		t.Fatalf("supplier duplicate status=%q err=%v", status, err)
	}
}
