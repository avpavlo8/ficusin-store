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
