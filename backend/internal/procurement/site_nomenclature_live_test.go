package procurement

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestInvoiceOnlyLineLinksToExactSiteVariantWithoutSaby(t *testing.T) {
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
	code := fmt.Sprintf("%d", unique)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	var supplier, product, variant, order, alias, line, actor int64
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM procurement_orders WHERE id=$1", order)
		_, _ = pool.Exec(ctx, "DELETE FROM procurement_suppliers WHERE id=$1", supplier)
		_, _ = pool.Exec(ctx, "DELETE FROM products WHERE id=$1", product)
		_, _ = pool.Exec(ctx, "DELETE FROM customers WHERE id=$1", actor)
	}()
	must(pool.QueryRow(ctx, `INSERT INTO customers(email,phone,full_name,consent_at) VALUES($1,$2,'Site catalogue buyer',NOW()) RETURNING id`, code+"@example.invalid", fmt.Sprintf("+79%09d", unique%1000000000)).Scan(&actor))
	must(pool.QueryRow(ctx, `INSERT INTO products(name,slug,status,catalog_section) VALUES('Aglaonema for site',$1,'draft','plants') RETURNING id`, code).Scan(&product))
	must(pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,sku,label,base_price_minor,is_active) VALUES($1,$2,'D12',100000,1) RETURNING id`, product, code).Scan(&variant))
	must(pool.QueryRow(ctx, `INSERT INTO procurement_suppliers(name,kind,default_currency) VALUES($1,'domestic','RUB') RETURNING id`, code).Scan(&supplier))
	must(pool.QueryRow(ctx, `INSERT INTO procurement_orders(supplier_id,source_kind,currency,status,created_by) VALUES($1,'invoice','RUB','review',$2) RETURNING id`, supplier, actor).Scan(&order))
	must(pool.QueryRow(ctx, `INSERT INTO procurement_supplier_aliases(supplier_id,raw_name,match_status) VALUES($1,'Invoice plant','unmatched') RETURNING id`, supplier).Scan(&alias))
	must(pool.QueryRow(ctx, `INSERT INTO procurement_order_lines(procurement_order_id,supplier_alias_id,raw_name,ordered_qty,invoiced_qty,unit_price,reconciliation_status,match_status) VALUES($1,$2,'Invoice plant',0,8,650,'added','unmatched') RETURNING id`, order, alias).Scan(&line))
	matches, err := store.SearchSiteNomenclature(ctx, code)
	must(err)
	if len(matches) != 1 || matches[0].VariantID != variant || matches[0].SabyID != "" {
		t.Fatalf("unexpected exact variant result: %+v", matches)
	}
	_, err = store.ResolveAlias(ctx, Actor{CustomerID: actor}, alias, AliasResolution{MatchStatus: "confirmed", VariantID: variant, OrderLineID: line})
	must(err)
	var accepted bool
	must(pool.QueryRow(ctx, `SELECT comparison_accepted AND match_status='confirmed' AND canonical_variant_id=$2 AND saby_id IS NULL FROM procurement_order_lines WHERE id=$1`, line, variant).Scan(&accepted))
	if !accepted {
		t.Fatal("invoice-only row was not confirmed against the exact site variant")
	}
	_, err = pool.Exec(ctx, `UPDATE procurement_orders SET status='ready_to_receive',calculated_at=NOW(),calculation_version=(SELECT version FROM procurement_pricing_settings WHERE id=1) WHERE id=$1`, order)
	must(err)
	_, err = pool.Exec(ctx, `UPDATE procurement_order_lines SET proposed_retail_rub=1700,baseline_site_price_minor=100000,channel_quotes='{"site":{"priceRub":1700,"breakEvenRub":650,"contributionRub":100}}' WHERE id=$1`, line)
	must(err)
	batch, err := store.PrepareBatch(ctx, Actor{CustomerID: actor}, order, "prices", []string{"site"})
	must(err)
	if len(batch.Items) != 1 || batch.Items[0].Channel != "site" || batch.Items[0].LineID != line {
		t.Fatalf("site-only variant missing from price proposal: %+v", batch.Items)
	}

	// Old invoice rows may predate the supplier alias link. They can still be
	// matched through the same resolution transaction as new imports.
	var legacyLine int64
	must(pool.QueryRow(ctx, `INSERT INTO procurement_order_lines(procurement_order_id,raw_name,ordered_qty,invoiced_qty,unit_price,reconciliation_status,match_status)
VALUES($1,'Legacy invoice plant',0,2,500,'added','new_product') RETURNING id`, order).Scan(&legacyLine))
	_, err = store.UpdateOrderLine(ctx, Actor{CustomerID: actor}, legacyLine, OrderLineUpdate{SiteVariantID: &variant})
	must(err)
	must(pool.QueryRow(ctx, `SELECT comparison_accepted AND match_status='confirmed' AND canonical_variant_id=$2
  AND supplier_alias_id IS NOT NULL AND saby_id IS NULL FROM procurement_order_lines WHERE id=$1`, legacyLine, variant).Scan(&accepted))
	if !accepted {
		t.Fatal("legacy invoice-only row without an alias could not select the site variant")
	}

}
