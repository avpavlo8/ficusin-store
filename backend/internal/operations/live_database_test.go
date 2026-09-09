package operations

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestProbeDetectsAndClearsReservationMismatchOnLiveDatabase(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	var productID, variantID, warehouseID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO products(name, slug, status, category_id)
		SELECT 'CI operations product', $1, 'draft', id
		FROM categories WHERE slug='accessories' RETURNING id
	`, "ci-operations-"+suffix).Scan(&productID); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO product_variants(product_id, label, base_price_minor)
		VALUES ($1, 'CI operations variant', 10000) RETURNING id
	`, productID).Scan(&variantID); err != nil {
		t.Fatalf("seed variant: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO warehouses(saby_id, name, city, address)
		VALUES ($1, 'CI operations warehouse', 'CI', 'CI') RETURNING id
	`, "ci-operations-"+suffix).Scan(&warehouseID); err != nil {
		t.Fatalf("seed warehouse: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM products WHERE id=$1", productID)
		_, _ = pool.Exec(ctx, "DELETE FROM warehouses WHERE id=$1", warehouseID)
	}()
	if _, err := pool.Exec(ctx, `
		INSERT INTO inventory(warehouse_id, variant_id, available_qty, reserved_qty)
		VALUES ($1, $2, 5, 1)
	`, warehouseID, variantID); err != nil {
		t.Fatalf("seed mismatch: %v", err)
	}

	probe := NewProbe(pool)
	snapshot, err := probe.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !containsCheck(snapshot.Checks, "reservation_ledger_mismatch") {
		t.Fatalf("reservation mismatch was not detected: %#v", snapshot)
	}
	if _, err := pool.Exec(ctx, "UPDATE inventory SET reserved_qty=0 WHERE warehouse_id=$1 AND variant_id=$2", warehouseID, variantID); err != nil {
		t.Fatal(err)
	}
	snapshot, err = probe.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if containsCheck(snapshot.Checks, "reservation_ledger_mismatch") {
		t.Fatalf("repaired mismatch is still reported: %#v", snapshot)
	}
}

func containsCheck(checks []Check, code string) bool {
	for _, check := range checks {
		if check.Code == code {
			return true
		}
	}
	return false
}
