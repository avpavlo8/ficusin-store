package procurement

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestStage13AliasUpsertExistingDecimalDimensionsOnLiveDatabase reproduces the
// production path hit by Holland invoice 8208247: the alias already exists and
// the parsed PDF supplies float64 pot/height dimensions. The conflict fallback
// must compare those values as NUMERIC, not let PostgreSQL infer integer
// parameters from COALESCE(..., -1).
func TestStage13AliasUpsertExistingDecimalDimensionsOnLiveDatabase(t *testing.T) {
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
	var supplierID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO procurement_suppliers(name, kind, country_code, default_currency)
		VALUES($1, 'international', 'NL', 'EUR') RETURNING id
	`, fmt.Sprintf("Alias conflict %d", unique)).Scan(&supplierID); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO procurement_supplier_aliases(
			supplier_id, raw_name, normalized_name, supplier_article,
			pot_diameter_cm, height_cm, match_status, occurrences
		) VALUES($1, 'Beaucarnea Recur Bol', 'nolina recur bol', '', 6, 20, 'unmatched', 2)
	`, supplierID); err != nil {
		t.Fatal(err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	pot, height := 6.0, 20.0
	aliasID, _, _, err := upsertAlias(ctx, tx, supplierID, ParsedLine{
		RawName:       "Beaucarnea Recur Bol",
		PotDiameterCM: &pot,
		HeightCM:      &height,
	}, nil)
	if err != nil {
		t.Fatalf("upsert existing alias with parsed dimensions: %v", err)
	}
	if aliasID == 0 {
		t.Fatal("expected existing alias id")
	}
}
