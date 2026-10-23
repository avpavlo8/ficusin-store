package procurement

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// All fixtures use an isolated CRM test database and never call Saby.
func invoiceFirstFixture(t *testing.T) (context.Context, *pgxpool.Pool, *PostgresStore, Actor, int64, string) {
	t.Helper()
	dsn := os.Getenv("CRM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("CRM_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	key := fmt.Sprintf("invoice-first-%d", time.Now().UnixNano())
	var actorID, supplierID int64
	if err = pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at)
 VALUES($1,$2,'','Invoice owner',CURRENT_TIMESTAMP) RETURNING id`, key+"@example.invalid", key).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_suppliers(name,kind,default_currency)
 VALUES($1,'domestic','RUB') RETURNING id`, key).Scan(&supplierID); err != nil {
		t.Fatal(err)
	}
	return ctx, pool, NewPostgresStore(pool), Actor{CustomerID: actorID, Role: "owner"}, supplierID, key
}

func TestInvoiceFirstCompositionOnLiveDatabase(t *testing.T) {
	for _, mode := range []string{"new", "empty_order", "planned", "arithmetic_error"} {
		t.Run(mode, func(t *testing.T) {
			ctx, pool, store, actor, supplierID, key := invoiceFirstFixture(t)
			var orderID int64
			if mode == "empty_order" || mode == "planned" {
				if err := pool.QueryRow(ctx, `INSERT INTO procurement_orders(supplier_id,order_number,source_kind,currency,status,created_by)
    VALUES($1,$2,'manual','RUB','draft',$3) RETURNING id`, supplierID, key, actor.CustomerID).Scan(&orderID); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "planned" {
				if _, err := pool.Exec(ctx, `INSERT INTO procurement_order_lines(procurement_order_id,raw_name,supplier_article,ordered_qty,expected_unit_price,match_status,reconciliation_status)
    VALUES($1,'Planned plant','A',2,300,'unmatched','planned')`, orderID); err != nil {
					t.Fatal(err)
				}
			}
			parsed := ParsedDocument{ParserKind: "domestic_payment_invoice", DocumentNumber: key, Currency: "RUB", ArithmeticOK: mode != "arithmetic_error",
				ProductSubtotal: 1400, DocumentTotal: 1400, CalculatedTotal: 1400,
				Lines: []ParsedLine{{RawName: "Invoice plant A", SupplierArticle: "A", Quantity: 2, UnitPrice: 350, LineTotal: 700, SourcePage: 1, SourceLine: 1},
					{RawName: "Invoice plant B", SupplierArticle: "B", Quantity: 2, UnitPrice: 350, LineTotal: 700, SourcePage: 1, SourceLine: 2}}}
			upload := DocumentUpload{OrderID: orderID, SupplierID: supplierID, FileName: "invoice.pdf", ContentType: "application/pdf", Content: []byte("%PDF-" + key)}
			imported, err := store.ImportDocument(ctx, actor, upload, parsed)
			if err != nil {
				t.Fatal(err)
			}
			detail, err := store.OrderDetail(ctx, imported.Order.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(detail.Lines) != 2 {
				t.Fatalf("lines=%+v", detail.Lines)
			}
			if mode == "planned" {
				if detail.Order.PendingReview != 2 || detail.Lines[0].ReconciliationStatus != "changed" || detail.Lines[1].ReconciliationStatus != "added" {
					t.Fatalf("plan discrepancies lost: %+v", detail)
				}
			} else {
				if detail.Order.Lines != 2 || detail.Order.Units != 4 || detail.Order.Total != 1400 || detail.Validation.ComparisonMismatch != 0 {
					t.Fatalf("invoice composition requires manual acceptance: %+v", detail)
				}
				for _, line := range detail.Lines {
					if line.OrderedQuantity != 0 || line.ReconciliationStatus != "matched" || line.ComparisonMismatch {
						t.Fatalf("invented a plan or discrepancy: %+v", line)
					}
				}
			}
			if detail.Validation.CanCalculate {
				t.Fatal("unmatched invoice products bypassed identity review")
			}
			if mode == "arithmetic_error" && detail.Validation.ArithmeticMismatch == 0 {
				t.Fatal("arithmetic error bypassed review")
			}
			duplicate, err := store.ImportDocument(ctx, actor, upload, parsed)
			if err != nil || !duplicate.Duplicate || duplicate.Order.ID != imported.Order.ID {
				t.Fatalf("duplicate=%+v err=%v", duplicate, err)
			}
			// Replacing an invoice-only document must still yield accepted composition.
			if mode == "new" {
				upload.OrderID = imported.Order.ID
				upload.Content = append(upload.Content, '2')
				parsed.Lines[0].Quantity = 3
				parsed.Lines[0].LineTotal = 1050
				parsed.ProductSubtotal = 1750
				parsed.DocumentTotal = 1750
				parsed.CalculatedTotal = 1750
				if _, err = store.ImportDocument(ctx, actor, upload, parsed); err != nil {
					t.Fatal(err)
				}
				replaced, err := store.OrderDetail(ctx, imported.Order.ID)
				if err != nil || replaced.Order.Units != 5 || replaced.Order.Total != 1750 || replaced.Validation.ComparisonMismatch != 0 {
					t.Fatalf("replacement=%+v err=%v", replaced, err)
				}
			}
		})
	}
}

func TestInvoiceFirstSabyPricesOnLiveDatabase(t *testing.T) {
	ctx, pool, store, actor, supplierID, key := invoiceFirstFixture(t)
	rawID, siteID := key+"-raw", key+"-site"
	for _, id := range []string{rawID, siteID} {
		if _, err := pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,name,balance,price_minor)
   VALUES($1,$1,$1,0,0)`, id); err != nil {
			t.Fatal(err)
		}
	}
	var productID, variantID int64
	if err := pool.QueryRow(ctx, `INSERT INTO products(name,slug,status,catalog_section) VALUES($1,$1,'draft','plants') RETURNING id`, key).Scan(&productID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,saby_id,label,base_price_minor,is_active)
  VALUES($1,$2,'D10',0,1) RETURNING id`, productID, siteID).Scan(&variantID); err != nil {
		t.Fatal(err)
	}
	parsed := ParsedDocument{ParserKind: "domestic_payment_invoice", DocumentNumber: key, Currency: "RUB", ArithmeticOK: true,
		ProductSubtotal: 1400, DocumentTotal: 1400, CalculatedTotal: 1400,
		Lines: []ParsedLine{{RawName: "New raw plant", Quantity: 2, UnitPrice: 350, LineTotal: 700, SourcePage: 1, SourceLine: 1},
			{RawName: "New site plant", Quantity: 2, UnitPrice: 350, LineTotal: 700, SourcePage: 1, SourceLine: 2}}}
	imported, err := store.ImportDocument(ctx, actor, DocumentUpload{SupplierID: supplierID, FileName: "first.pdf", ContentType: "application/pdf", Content: []byte("%PDF-" + key)}, parsed)
	if err != nil {
		t.Fatal(err)
	}
	for name, id := range map[string]string{"New raw plant": rawID, "New site plant": siteID} {
		var aliasID int64
		if err = pool.QueryRow(ctx, `SELECT id FROM procurement_supplier_aliases WHERE supplier_id=$1 AND raw_name=$2`, supplierID, name).Scan(&aliasID); err != nil {
			t.Fatal(err)
		}
		if _, err = store.ResolveAlias(ctx, actor, aliasID, AliasResolution{MatchStatus: "confirmed", SabyID: id}); err != nil {
			t.Fatal(err)
		}
	}
	orderID := imported.Order.ID
	detail, err := store.CalculateOrder(ctx, actor, orderID, CalculationInput{})
	if err != nil {
		t.Fatal(err)
	}
	var rawPrice int64
	for _, line := range detail.Lines {
		if line.SabyID == rawID && line.ProposedRetailRUB != nil {
			rawPrice = *line.ProposedRetailRUB
		}
	}
	if rawPrice <= 0 {
		t.Fatalf("no first price calculated: %+v", detail)
	}
	approval := []SabyFirstPrice{{SabyID: rawID, PriceRUB: rawPrice}}
	if _, err = store.FirstSabyPriceExportLines(ctx, actor, orderID, approval); err == nil {
		t.Fatal("site price exported without approval")
	}
	batch, err := store.PrepareBatch(ctx, actor, orderID, "prices", []string{"site"})
	if err != nil || len(batch.Items) != 1 {
		t.Fatalf("zero site price preview=%+v err=%v", batch, err)
	}
	if _, err = store.ApproveBatch(ctx, actor, batch.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ApprovedSabyPriceExportLines(ctx, orderID); err == nil {
		t.Fatal("new raw price silently omitted or exported without confirmation")
	}
	if _, err = store.FirstSabyPriceExportLines(ctx, actor, orderID, []SabyFirstPrice{{SabyID: rawID, PriceRUB: rawPrice + 1}}); err == nil {
		t.Fatal("stale first price approval accepted")
	}
	// A completed receipt must not require reopening or a duplicate receipt to export prices.
	if _, err = pool.Exec(ctx, `UPDATE procurement_orders SET status='received' WHERE id=$1`, orderID); err != nil {
		t.Fatal(err)
	}
	lines, err := store.FirstSabyPriceExportLines(ctx, actor, orderID, approval)
	if err != nil || len(lines) != 2 {
		t.Fatalf("first prices=%+v err=%v", lines, err)
	}
	if _, count, err := BuildSabyPriceXLSX(lines); err != nil || count != 2 {
		t.Fatalf("file count=%d err=%v", count, err)
	}
	var externalPrice int64
	var count int
	if err = pool.QueryRow(ctx, `SELECT price_minor FROM saby_nomenclature WHERE saby_id=$1`, rawID).Scan(&externalPrice); err != nil || externalPrice != 0 {
		t.Fatalf("export changed Saby price=%d err=%v", externalPrice, err)
	}
	if err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM procurement_action_items item JOIN procurement_action_batches batch ON batch.id=item.batch_id WHERE batch.procurement_order_id=$1 AND item.channel<>'site'`, orderID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("export queued external changes=%d err=%v", count, err)
	}
	// Missing quotes and changes to the exact Saby identity must invalidate confirmation.
	if _, err = pool.Exec(ctx, `UPDATE procurement_order_lines SET channel_quotes='{}' WHERE procurement_order_id=$1 AND saby_id=$2`, orderID, rawID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.FirstSabyPriceExportLines(ctx, actor, orderID, approval); err == nil {
		t.Fatal("missing quote exported")
	}
}

func TestInvoiceFirstLateSiteVariantOnLiveDatabase(t *testing.T) {
	ctx, pool, store, actor, supplierID, key := invoiceFirstFixture(t)
	if _, err := pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,name,balance,price_minor) VALUES($1,$1,$1,0,0)`, key); err != nil {
		t.Fatal(err)
	}
	parsed := ParsedDocument{ParserKind: "domestic_payment_invoice", DocumentNumber: key, Currency: "RUB", ArithmeticOK: true, ProductSubtotal: 700, DocumentTotal: 700, CalculatedTotal: 700, Lines: []ParsedLine{{RawName: "Late site plant", SourcePage: 1, SourceLine: 1, Quantity: 2, UnitPrice: 350, LineTotal: 700}}}
	imported, err := store.ImportDocument(ctx, actor, DocumentUpload{SupplierID: supplierID, FileName: "late.pdf", ContentType: "application/pdf", Content: []byte("%PDF-" + key)}, parsed)
	if err != nil {
		t.Fatal(err)
	}
	var aliasID int64
	if err = pool.QueryRow(ctx, `SELECT id FROM procurement_supplier_aliases WHERE supplier_id=$1`, supplierID).Scan(&aliasID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ResolveAlias(ctx, actor, aliasID, AliasResolution{MatchStatus: "confirmed", SabyID: key}); err != nil {
		t.Fatal(err)
	}
	var productID, variantID int64
	if err = pool.QueryRow(ctx, `INSERT INTO products(name,slug,status,catalog_section) VALUES($1,$1,'draft','plants') RETURNING id`, key).Scan(&productID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,saby_id,label,base_price_minor,is_active) VALUES($1,$2,'D10',0,1) RETURNING id`, productID, key).Scan(&variantID); err != nil {
		t.Fatal(err)
	}
	detail, err := store.CalculateOrder(ctx, actor, imported.Order.ID, CalculationInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Lines) != 1 || detail.Lines[0].CanonicalVariantID == nil || *detail.Lines[0].CanonicalVariantID != variantID {
		t.Fatalf("late SKU not reconciled: %+v", detail.Lines)
	}
	batch, err := store.PrepareBatch(ctx, actor, imported.Order.ID, "prices", []string{"site"})
	if err != nil || len(batch.Items) != 1 {
		t.Fatalf("first price not offered: %+v err=%v", batch, err)
	}
}
