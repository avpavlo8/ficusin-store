package procurement

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"math"
	"os"
	"strings"
	"testing"
	"time"
)

func TestInvoiceGroupPreservesRowsQuantitiesAndAliasIdentity(t *testing.T) {
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
	saby := fmt.Sprintf("group-cactus-%d", unique)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,name,balance,section_path,seen_at) VALUES($1,$1,'Кактус микс D5',0,ARRAY['Цветы'],now())`, saby)
	must(err)
	var actorID, supplierID, orderID, planID, planAliasID int64
	must(pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at) VALUES($1,$2,'','Invoice group test',now()) RETURNING id`, fmt.Sprintf("group-%d@example.invalid", unique), fmt.Sprintf("+76%09d", unique%1000000000)).Scan(&actorID))
	must(pool.QueryRow(ctx, `INSERT INTO procurement_suppliers(name,kind,default_currency) VALUES($1,'international','EUR') RETURNING id`, fmt.Sprintf("Group %d", unique)).Scan(&supplierID))
	must(pool.QueryRow(ctx, `INSERT INTO procurement_supplier_aliases(supplier_id,raw_name,normalized_name,supplier_article,pot_diameter_cm,height_cm,match_status)
 VALUES($1,'Кактус микс','кактус микс','CACTUS',5,10,'unmatched') RETURNING id`, supplierID).Scan(&planAliasID))
	must(pool.QueryRow(ctx, `INSERT INTO procurement_orders(supplier_id,source_kind,currency,status,created_by) VALUES($1,'recommendation','EUR','ordered',$2) RETURNING id`, supplierID, actorID).Scan(&orderID))
	must(pool.QueryRow(ctx, `INSERT INTO procurement_order_lines(procurement_order_id,supplier_alias_id,saby_id,raw_name,supplier_article,ordered_qty,expected_unit_price,load_unit,pot_diameter_cm,height_cm,match_status,reconciliation_status)
 VALUES($1,$2,$3,'Кактус микс','CACTUS',60,.87,'shelf',5,10,'confirmed','planned') RETURNING id`, orderID, planAliasID, saby).Scan(&planID))
	actor := Actor{CustomerID: actorID, Role: "owner"}
	pot, height := 5.0, 10.0
	parsed := ParsedDocument{ParserKind: "holland_packing_list", DocumentNumber: fmt.Sprintf("GROUP-%d", unique), Currency: "EUR", ArithmeticOK: true, ProductSubtotal: 52.2, DocumentTotal: 52.2, CalculatedTotal: 52.2}
	for i, qty := range []int{10, 20, 30} {
		parsed.Lines = append(parsed.Lines, ParsedLine{SourcePage: 1, SourceLine: i + 1, RawName: "Cactus Mix", Quantity: qty, UnitPrice: .87, LineTotal: float64(qty) * .87, LoadUnit: "shelf", PotDiameterCM: &pot, HeightCM: &height})
	}
	_, err = store.ImportDocument(ctx, actor, DocumentUpload{SupplierID: supplierID, OrderID: orderID, FileName: "cactus.pdf", ContentType: "application/pdf", Content: []byte(fmt.Sprintf("%%PDF-group-%d", unique))}, parsed)
	must(err)
	rows, err := pool.Query(ctx, `SELECT id FROM procurement_order_lines WHERE procurement_order_id=$1 AND ordered_qty=0 AND reconciliation_status='added' ORDER BY source_line`, orderID)
	must(err)
	ids := []int64{}
	for rows.Next() {
		var id int64
		must(rows.Scan(&id))
		ids = append(ids, id)
	}
	rows.Close()
	if len(ids) != 3 {
		t.Fatalf("invoice rows %v", ids)
	}
	var invoiceAlias int64
	must(pool.QueryRow(ctx, `SELECT supplier_alias_id FROM procurement_order_lines WHERE id=$1`, ids[0]).Scan(&invoiceAlias))
	shadow := map[string]int64{}
	for _, status := range []string{"review", "received"} {
		var order, line int64
		must(pool.QueryRow(ctx, `INSERT INTO procurement_orders(supplier_id,source_kind,currency,status,created_by) VALUES($1,'recommendation','EUR',$2,$3) RETURNING id`, supplierID, status, actorID).Scan(&order))
		must(pool.QueryRow(ctx, `INSERT INTO procurement_order_lines(procurement_order_id,supplier_alias_id,raw_name,ordered_qty,load_unit,match_status,reconciliation_status) VALUES($1,$2,'Cactus Mix',1,'shelf','unmatched','planned') RETURNING id`, order, invoiceAlias).Scan(&line))
		shadow[status] = line
	}
	_, err = store.UpdateOrderLine(ctx, actor, planID, OrderLineUpdate{InvoiceLineID: &ids[0]})
	must(err)
	detail, err := store.OrderDetail(ctx, orderID)
	must(err)
	if len(detail.Lines) != 1 || detail.Lines[0].InvoicedQuantity == nil || *detail.Lines[0].InvoicedQuantity != 60 || detail.Lines[0].ReconciliationStatus != "matched" || math.Abs(detail.Order.Total-52.2) > .00001 {
		t.Fatalf("aggregate %+v lines=%+v", detail.Order, detail.Lines)
	}
	var parts []struct {
		Quantity             int
		UnitPrice, LineTotal float64
	}
	must(json.Unmarshal(detail.Lines[0].InvoiceComponents, &parts))
	if len(parts) != 3 || parts[0].Quantity != 10 || parts[1].Quantity != 20 || parts[2].Quantity != 30 {
		t.Fatalf("lost source rows %+v", parts)
	}
	for status, id := range shadow {
		var card, match string
		must(pool.QueryRow(ctx, `SELECT COALESCE(saby_id,''),match_status FROM procurement_order_lines WHERE id=$1`, id).Scan(&card, &match))
		if status == "review" && (card != saby || match != "confirmed") {
			t.Fatalf("active alias not propagated: %s %s", card, match)
		}
		if status == "received" && (card != "" || match != "unmatched") {
			t.Fatal("closed purchase was rewritten")
		}
	}
	var unresolved int
	must(pool.QueryRow(ctx, `SELECT COUNT(*) FROM procurement_supplier_aliases WHERE supplier_id=$1 AND match_status<>'confirmed'`, supplierID).Scan(&unresolved))
	if unresolved != 0 {
		t.Fatalf("unresolved aliases %d", unresolved)
	}
	// Simulate the stale plan-name alias left by the previous version.
	_, err = pool.Exec(ctx, `UPDATE procurement_supplier_aliases SET match_status='unmatched',matched_saby_id=NULL WHERE id=$1`, planAliasID)
	must(err)
	migration, err := os.ReadFile("../../../timeweb/migrations/20261015_procurement_invoice_groups.sql")
	must(err)
	repair := string(migration[strings.Index(string(migration), "WITH evidence AS"):])
	for pass := 0; pass < 2; pass++ {
		_, err = pool.Exec(ctx, repair)
		must(err)
	}
	var repairedStatus string
	must(pool.QueryRow(ctx, `SELECT match_status FROM procurement_supplier_aliases WHERE id=$1`, planAliasID).Scan(&repairedStatus))
	if repairedStatus != "confirmed" {
		t.Fatal("stale queue entry not repaired")
	}
	if _, err = store.UpdateOrderLine(ctx, actor, planID, OrderLineUpdate{InvoiceLineIDs: ids[1:]}); err == nil {
		t.Fatal("repeated source rows must be rejected")
	}
	after, err := store.OrderDetail(ctx, orderID)
	must(err)
	if after.Order.Units != 60 || math.Abs(after.Order.Total-52.2) > .00001 {
		t.Fatal("retry duplicated quantities")
	}
	// A fresh document revision reuses the learned supplier alias. Group history
	// must retain all old PDF rows after the active invoice is replaced.
	parsed.DocumentNumber += "-REV2"
	_, err = store.ImportDocument(ctx, actor, DocumentUpload{SupplierID: supplierID, OrderID: orderID, FileName: "cactus-v2.pdf", ContentType: "application/pdf", Content: []byte(fmt.Sprintf("%%PDF-group-v2-%d", unique))}, parsed)
	must(err)
	var archived int
	must(pool.QueryRow(ctx, `SELECT jsonb_array_length(invoice_components) FROM procurement_invoice_line_history WHERE procurement_order_line_id=$1 ORDER BY id DESC LIMIT 1`, planID).Scan(&archived))
	if archived != 3 {
		t.Fatalf("history lost components: %d", archived)
	}
	var wrong int
	must(pool.QueryRow(ctx, `SELECT COUNT(*) FROM procurement_order_lines WHERE procurement_order_id=$1 AND reconciliation_status<>'superseded' AND (saby_id IS DISTINCT FROM $2 OR match_status<>'confirmed')`, orderID, saby).Scan(&wrong))
	if wrong != 0 {
		t.Fatalf("learned alias not reused: %d", wrong)
	}

	// Explicitly select several previously unknown rows, preserving mixed prices.
	parsed.DocumentNumber += "-MIXED"
	total := 0.0
	for i, price := range []float64{.87, .90, 1.10} {
		parsed.Lines[i].RawName = "Cactus Assorted"
		parsed.Lines[i].UnitPrice = price
		parsed.Lines[i].LineTotal = float64(parsed.Lines[i].Quantity) * price
		total += parsed.Lines[i].LineTotal
	}
	parsed.ProductSubtotal = total
	parsed.DocumentTotal = total
	parsed.CalculatedTotal = total
	_, err = store.ImportDocument(ctx, actor, DocumentUpload{SupplierID: supplierID, OrderID: orderID, FileName: "mixed.pdf", ContentType: "application/pdf", Content: []byte(fmt.Sprintf("%%PDF-mixed-%d", unique))}, parsed)
	must(err)
	rows, err = pool.Query(ctx, `SELECT id FROM procurement_order_lines WHERE procurement_order_id=$1 AND reconciliation_status='added' ORDER BY source_line`, orderID)
	must(err)
	ids = nil
	for rows.Next() {
		var id int64
		must(rows.Scan(&id))
		ids = append(ids, id)
	}
	rows.Close()
	if len(ids) != 3 {
		t.Fatalf("expected 3 unknown mixed-price rows, got %v", ids)
	}
	bad := append(append([]int64{}, ids...), planID)
	if _, err = store.UpdateOrderLine(ctx, actor, planID, OrderLineUpdate{InvoiceLineIDs: bad}); err == nil {
		t.Fatal("invalid selection must roll back")
	}
	detail, err = store.UpdateOrderLine(ctx, actor, planID, OrderLineUpdate{InvoiceLineIDs: ids})
	must(err)
	if len(detail.Lines) != 1 || detail.Order.Units != 60 || math.Abs(detail.Order.Total-59.7) > .000001 || detail.Lines[0].ReconciliationStatus != "changed" || math.Abs(detail.Lines[0].UnitPrice-.995) > .0000001 {
		t.Fatalf("weighted price lost facts: %+v %+v", detail.Order, detail.Lines)
	}
	must(json.Unmarshal(detail.Lines[0].InvoiceComponents, &parts))
	if len(parts) != 3 || parts[1].UnitPrice != .90 || parts[2].LineTotal != 33 {
		t.Fatalf("mixed source prices lost: %+v", parts)
	}
	// Swapping an aggregated assignment must move every source row and preserve
	// both planned products, rather than leaving a stale aggregate on the first.
	otherSaby := saby + "-other"
	_, err = pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,name,balance,section_path,seen_at) VALUES($1,$1,'Другой кактус',0,ARRAY['Цветы'],now())`, otherSaby)
	must(err)
	var otherAlias, otherPlan, doc int64
	must(pool.QueryRow(ctx, `INSERT INTO procurement_supplier_aliases(supplier_id,raw_name,normalized_name,matched_saby_id,match_status) VALUES($1,'Other Cactus','other cactus',$2,'confirmed') RETURNING id`, supplierID, otherSaby).Scan(&otherAlias))
	must(pool.QueryRow(ctx, `SELECT procurement_document_id FROM procurement_order_lines WHERE id=$1`, planID).Scan(&doc))
	must(pool.QueryRow(ctx, `INSERT INTO procurement_order_lines(procurement_order_id,procurement_document_id,supplier_alias_id,saby_id,raw_name,invoice_raw_name,ordered_qty,invoiced_qty,expected_unit_price,unit_price,line_total,load_unit,pot_diameter_cm,height_cm,match_status,reconciliation_status,source_page,source_line)
 VALUES($1,$2,$3,$4,'Другой кактус','Other Cactus',5,5,2,2,10,'shelf',5,10,'confirmed','matched',1,4) RETURNING id`, orderID, doc, otherAlias, otherSaby).Scan(&otherPlan))
	detail, err = store.UpdateOrderLine(ctx, actor, planID, OrderLineUpdate{InvoiceLineID: &otherPlan})
	must(err)
	for _, line := range detail.Lines {
		switch line.ID {
		case planID:
			if line.SabyID != saby || *line.InvoicedQuantity != 5 {
				t.Fatalf("first plan identity changed: %+v", line)
			}
		case otherPlan:
			if line.SabyID != otherSaby || *line.InvoicedQuantity != 60 {
				t.Fatalf("second plan identity changed: %+v", line)
			}
			must(json.Unmarshal(line.InvoiceComponents, &parts))
			if len(parts) != 3 {
				t.Fatal("swap lost grouped PDF rows")
			}
		}
	}
}
