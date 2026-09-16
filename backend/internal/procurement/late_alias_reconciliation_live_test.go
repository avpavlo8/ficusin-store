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

func TestManualInvoicePairOverridesWrongAliasAndPreservesPlanPrice(t *testing.T) {
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
	plannedSaby := fmt.Sprintf("manual-carmona-%d", unique)
	wrongSaby := fmt.Sprintf("manual-wrong-%d", unique)
	for id, name := range map[string]string{plannedSaby: "Bonsai Carmona D10", wrongSaby: "Wrong card"} {
		if _, err = pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,name,balance,section_path,seen_at)
			VALUES($1,$1,$2,0,ARRAY['Цветы'],CURRENT_TIMESTAMP)`, id, name); err != nil {
			t.Fatal(err)
		}
	}
	var actorID int64
	if err = pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at)
		VALUES($1,$2,'','Manual pair owner',CURRENT_TIMESTAMP) RETURNING id`, fmt.Sprintf("manual-pair-%d@example.invalid", unique), fmt.Sprintf("+77%09d", unique%1000000000)).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	var supplierID int64
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_suppliers(name,kind,country_code,default_currency)
		VALUES($1,'international','NL','EUR') RETURNING id`, fmt.Sprintf("Manual pair supplier %d", unique)).Scan(&supplierID); err != nil {
		t.Fatal(err)
	}
	var orderID, plannedID int64
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_orders(supplier_id,order_number,source_kind,currency,status,created_by)
		VALUES($1,$2,'recommendation','EUR','ordered',$3) RETURNING id`, supplierID, fmt.Sprintf("PAIR-%d", unique), actorID).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_order_lines(procurement_order_id,saby_id,raw_name,ordered_qty,
		expected_unit_price,load_unit,pot_diameter_cm,height_cm,match_status,reconciliation_status)
		VALUES($1,$2,'Bonsai Carmona D10',10,4.25,'shelf',10,30,'confirmed','missing') RETURNING id`, orderID, plannedSaby).Scan(&plannedID); err != nil {
		t.Fatal(err)
	}
	var aliasID int64
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_supplier_aliases(supplier_id,raw_name,normalized_name,matched_saby_id,match_status,confidence,occurrences,last_seen_at)
		VALUES($1,'Bakarnie Nalina','bakarnie nalina',$2,'confirmed',1,1,CURRENT_DATE) RETURNING id`, supplierID, wrongSaby).Scan(&aliasID); err != nil {
		t.Fatal(err)
	}
	var documentID int64
	hash := fmt.Sprintf("%064x", unique)
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_documents(supplier_id,procurement_order_id,file_name,content_type,size_bytes,sha256,content,
		parser_kind,parser_version,parse_status,arithmetic_status,document_number,currency,line_count,unit_count,created_by)
		VALUES($1,$2,'manual-pair.pdf','application/pdf',8,$3,'12345678','holland_packing_list',1,'review','ok',$4,'EUR',1,12,$5) RETURNING id`,
		supplierID, orderID, hash, fmt.Sprintf("INV-PAIR-%d", unique), actorID).Scan(&documentID); err != nil {
		t.Fatal(err)
	}
	var addedID int64
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_order_lines(procurement_order_id,procurement_document_id,supplier_alias_id,saby_id,
		raw_name,invoice_raw_name,invoiced_qty,unit_price,line_total,load_unit,match_status,source_page,source_line,reconciliation_status)
		VALUES($1,$2,$3,$4,'Bakarnie Nalina','Bakarnie Nalina',12,4.50,54,'shelf','confirmed',1,1,'added') RETURNING id`,
		orderID, documentID, aliasID, wrongSaby).Scan(&addedID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.UpdateOrderLine(ctx, Actor{CustomerID: actorID, Role: "owner"}, plannedID, OrderLineUpdate{InvoiceLineID: &addedID}); err != nil {
		t.Fatal(err)
	}
	var status, mappedSaby, invoiceName string
	var expected, actual float64
	var qty int
	if err = pool.QueryRow(ctx, `SELECT reconciliation_status,expected_unit_price::DOUBLE PRECISION,unit_price::DOUBLE PRECISION,invoiced_qty,invoice_raw_name
		FROM procurement_order_lines WHERE id=$1`, plannedID).Scan(&status, &expected, &actual, &qty, &invoiceName); err != nil {
		t.Fatal(err)
	}
	if status != "changed" || expected != 4.25 || actual != 4.5 || qty != 12 || invoiceName != "Bakarnie Nalina" {
		t.Fatalf("paired line status=%q expected=%v actual=%v qty=%d invoice=%q", status, expected, actual, qty, invoiceName)
	}
	if err = pool.QueryRow(ctx, `SELECT matched_saby_id FROM procurement_supplier_aliases WHERE id=$1`, aliasID).Scan(&mappedSaby); err != nil || mappedSaby != plannedSaby {
		t.Fatalf("alias mapped=%q err=%v", mappedSaby, err)
	}
	if err = pool.QueryRow(ctx, `SELECT reconciliation_status FROM procurement_order_lines WHERE id=$1`, addedID).Scan(&status); err != nil || status != "superseded" {
		t.Fatalf("supplier-only line status=%q err=%v", status, err)
	}
}
