package procurement

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// A manager must be able to match a supplier name to an item that exists in
// Saby even when that item has not been imported into the public store yet.
// canonical_variant_id is optional for that procurement identity.
func TestStage13ResolveAliasAllowsSabyItemWithoutStoreVariantOnLiveDatabase(t *testing.T) {
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

	unique := time.Now().UnixNano()
	sabyID := fmt.Sprintf("raw-saby-%d", unique)
	var supplierID, aliasID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO procurement_suppliers(name,kind,country_code,default_currency)
		VALUES($1,'international','NL','EUR') RETURNING id
	`, fmt.Sprintf("Raw Saby supplier %d", unique)).Scan(&supplierID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO saby_nomenclature(saby_id,code,name,balance,section_path,seen_at,missing_since)
		VALUES($1,$2,'Фиттония тест',3,ARRAY['Цветы'],CURRENT_TIMESTAMP,NULL)
	`, sabyID, fmt.Sprintf("X%d", unique%10000000)); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO procurement_supplier_aliases(supplier_id,raw_name,normalized_name,match_status)
		VALUES($1,'Fitt Gem 3 Kl','fitt gem 3 kl','unmatched') RETURNING id
	`, supplierID).Scan(&aliasID); err != nil {
		t.Fatal(err)
	}

	store := NewPostgresStore(pool)
	if _, err := store.ResolveAlias(ctx, Actor{Role: "system"}, aliasID, AliasResolution{
		MatchStatus: "confirmed", SabyID: sabyID,
	}); err != nil {
		t.Fatalf("resolve raw Saby catalogue item: %v", err)
	}

	var matched string
	var canonical *int64
	if err := pool.QueryRow(ctx, `
		SELECT matched_saby_id,canonical_variant_id
		FROM procurement_supplier_aliases WHERE id=$1
	`, aliasID).Scan(&matched, &canonical); err != nil {
		t.Fatal(err)
	}
	if matched != sabyID {
		t.Fatalf("matched Saby ID = %q, want %q", matched, sabyID)
	}
	if canonical != nil {
		t.Fatalf("unexpected canonical variant for Saby-only item: %v", *canonical)
	}
	var linked bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM procurement_supplier_products WHERE supplier_id=$1 AND saby_id=$2)
	`, supplierID, sabyID).Scan(&linked); err != nil {
		t.Fatal(err)
	}
	if !linked {
		t.Fatal("supplier product link was not created")
	}
}


func TestStage13CalculationUsesConfirmedAliasIdentityForSabyOnlyProductOnLiveDatabase(t *testing.T) {
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

	unique := time.Now().UnixNano()
	oldSabyID := fmt.Sprintf("stale-saby-%d", unique)
	targetSabyID := fmt.Sprintf("target-saby-%d", unique)
	targetCode := fmt.Sprintf("X%d", 100000000+unique%800000000)
	var actorID, supplierID, aliasID, orderID, documentID, lineID int64

	if err = pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at)
		VALUES($1,$2,'','Alias pricing owner',CURRENT_TIMESTAMP) RETURNING id`,
		fmt.Sprintf("alias-pricing-%d@example.invalid", unique), fmt.Sprintf("+78%09d", unique%1000000000)).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_suppliers(name,kind,default_currency)
		VALUES($1,'domestic','RUB') RETURNING id`, fmt.Sprintf("Alias pricing supplier %d", unique)).Scan(&supplierID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,name,balance,price_minor,section_path,seen_at)
		VALUES($1,$2,'Старая временная карточка',0,0,ARRAY['Цветы маркетплейс'],CURRENT_TIMESTAMP),
		      ($3,$4,'Антуриум Блэк Бьюти CI',4,395000,ARRAY['Цветы маркетплейс'],CURRENT_TIMESTAMP)`,
		oldSabyID, oldSabyID, targetSabyID, targetCode); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_supplier_aliases(
		supplier_id,raw_name,normalized_name,matched_saby_id,match_status,confidence,last_seen_at
	) VALUES($1,'Anthurium Beauty Black','anthurium beauty black',$2,'confirmed',1,CURRENT_TIMESTAMP) RETURNING id`,
		supplierID, targetSabyID).Scan(&aliasID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO procurement_product_channels(saby_id,wb_nm_id,wb_vendor_code,updated_by)
		VALUES($1,$2,$3,$4)`, targetSabyID, 987654321, "антуриум блэк бьюти д17", actorID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_orders(
		supplier_id,order_number,source_kind,currency,status,created_by
	) VALUES($1,$2,'payment_invoice','RUB','review',$3) RETURNING id`,
		supplierID, fmt.Sprintf("ALIAS-PRICE-%d", unique), actorID).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_documents(
		supplier_id,procurement_order_id,file_name,content_type,size_bytes,sha256,content,
		parser_kind,parser_version,parse_status,arithmetic_status,document_number,currency,
		line_count,unit_count,product_subtotal,document_total,calculated_total,extracted_text,created_by,revision_no
	) VALUES($1,$2,'alias-price.pdf','application/pdf',4,$3,decode('25504446','hex'),
		'domestic_payment_invoice',1,'parsed','ok',$4,'RUB',1,4,4800,4800,4800,
		'Alias price synthetic invoice',$5,1) RETURNING id`,
		supplierID, orderID, fmt.Sprintf("%064x", unique), fmt.Sprintf("INV-ALIAS-%d", unique), actorID).Scan(&documentID); err != nil {
		t.Fatal(err)
	}
	// Reproduce production drift: the alias already points to the newly created
	// Saby product, while the invoice/order line still carries its old identity.
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_order_lines(
		procurement_order_id,procurement_document_id,supplier_alias_id,saby_id,raw_name,
		ordered_qty,invoiced_qty,unit_price,line_total,match_status,reconciliation_status
	) VALUES($1,$2,$3,$4,'Anthurium Beauty Black',4,4,1200,4800,'confirmed','matched') RETURNING id`,
		orderID, documentID, aliasID, oldSabyID).Scan(&lineID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM procurement_orders WHERE id=$1`, orderID)
		_, _ = pool.Exec(ctx, `DELETE FROM procurement_product_channels WHERE saby_id=$1`, targetSabyID)
		_, _ = pool.Exec(ctx, `DELETE FROM procurement_supplier_aliases WHERE id=$1`, aliasID)
		_, _ = pool.Exec(ctx, `DELETE FROM saby_nomenclature WHERE saby_id IN ($1,$2)`, oldSabyID, targetSabyID)
		_, _ = pool.Exec(ctx, `DELETE FROM procurement_suppliers WHERE id=$1`, supplierID)
		_, _ = pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, actorID)
	}()

	store := NewPostgresStore(pool)
	detail, err := store.CalculateOrder(ctx, Actor{CustomerID: actorID, Role: "owner"}, orderID, CalculationInput{})
	if err != nil {
		t.Fatal(err)
	}
	if detail.Order.Status != "ready_to_receive" {
		t.Fatalf("order status=%q, want ready_to_receive", detail.Order.Status)
	}

	var lineSabyID string
	var marketplace *float64
	if err = pool.QueryRow(ctx, `SELECT saby_id,proposed_marketplace_rub::DOUBLE PRECISION
		FROM procurement_order_lines WHERE id=$1`, lineID).Scan(&lineSabyID, &marketplace); err != nil {
		t.Fatal(err)
	}
	if lineSabyID != targetSabyID {
		t.Fatalf("calculation kept stale Saby identity: got %q want %q", lineSabyID, targetSabyID)
	}
	if marketplace == nil || *marketplace <= 0 {
		t.Fatalf("marketplace price was not calculated for remapped Saby-only product: %v", marketplace)
	}

	items, err := store.ListProducts(ctx, supplierID, targetCode)
	if err != nil || len(items) != 1 || items[0].SuggestedMarketplaceRUB == nil {
		t.Fatalf("product directory still has no marketplace price: items=%+v err=%v", items, err)
	}

	batch, err := store.PrepareBatch(ctx, Actor{CustomerID: actorID}, orderID, "prices", []string{"wb"})
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Items) != 1 || batch.Items[0].ProductCode != targetCode ||
		batch.Items[0].ExternalArticle != "987654321" || batch.Items[0].NewValue <= 0 {
		t.Fatalf("remapped product missing from WB price draft: %+v", batch.Items)
	}
}
