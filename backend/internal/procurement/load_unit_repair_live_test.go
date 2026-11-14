package procurement

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOrderDetailRepairsInvoiceLoadUnitsOnDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("CRM_TEST_DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("isolated TEST_DATABASE_URL or CRM_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	unique := time.Now().UnixNano()
	text := "Packinglist\nBox no: 1 CC-Kar\n20 Cactus Gem 0,72 14,40\nPot Ø: 5,5 Cm Height: 5 Cm\fPackinglist\n8 Yucca 2,91 23,28\nPot Ø: 14 Cm Height: 60 Cm\nBox no: 2 CC-Kar\n6 Anthu Black Love Plastic Free 11,27 67,62\nPot Ø: 17 Cm Height: 60 Cm\nSubtotal product 105,30\nTotal Amount € 105,30"
	parsed, err := ParseDocumentText(text)
	if err != nil {
		t.Fatal(err)
	}
	var orderID, documentID int64
	err = pool.QueryRow(ctx, `WITH supplier AS (
		INSERT INTO procurement_suppliers(name,kind,default_currency) VALUES($1,'international','EUR') RETURNING id
	), purchase AS (
		INSERT INTO procurement_orders(supplier_id,order_number,source_kind,currency,status)
		SELECT id,$1,'invoice','EUR','review' FROM supplier RETURNING id,supplier_id
	), document AS (
		INSERT INTO procurement_documents(supplier_id,procurement_order_id,file_name,content_type,size_bytes,sha256,content,parser_kind,parse_status,arithmetic_status,extracted_text)
		SELECT supplier_id,id,'repair.pdf','application/pdf',1,$2,'x','holland_packing_list','parsed','ok',$3 FROM purchase RETURNING id
	) SELECT purchase.id,document.id FROM purchase,document`, fmt.Sprintf("load-unit-%d", unique), fmt.Sprintf("%064x", unique), text).Scan(&orderID, &documentID)
	if err != nil {
		t.Fatal(err)
	}
	for index, value := range []string{"shelf", "", "7"} {
		line := parsed.Lines[index]
		_, err = pool.Exec(ctx, `INSERT INTO procurement_order_lines(procurement_order_id,procurement_document_id,raw_name,ordered_qty,invoiced_qty,unit_price,line_total,load_unit,match_status,source_line,pot_diameter_cm,height_cm)
			VALUES($1,$2,$3,$4,$4,$5,$6,$7,'deferred',$8,$9,$10)`, orderID, documentID, line.RawName, line.Quantity, line.UnitPrice, line.LineTotal, value, line.SourceLine, line.PotDiameterCM, line.HeightCM)
		if err != nil {
			t.Fatal(err)
		}
	}
	detail, err := NewPostgresStore(pool).OrderDetail(ctx, orderID)
	if err != nil {
		t.Fatal(err)
	}
	units := map[string]string{}
	for _, line := range detail.Lines {
		units[line.RawName] = line.LoadUnit
	}
	for index, want := range []string{"1", "1", "7"} {
		if got := units[parsed.Lines[index].RawName]; got != want {
			t.Fatalf("line %d trolley=%q, want %q; repair must preserve manual assignments", index, got, want)
		}
	}
	if detail.Validation.MissingLoadUnits != 0 || detail.Validation.TrolleyCount != 2 {
		t.Fatalf("validation ran before repair: %+v", detail.Validation)
	}
	var stored int
	if err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM procurement_order_lines WHERE procurement_order_id=$1 AND load_unit IN ('1','7')`, orderID).Scan(&stored); err != nil || stored != 3 {
		t.Fatalf("repair was not committed: rows=%d err=%v", stored, err)
	}
}
