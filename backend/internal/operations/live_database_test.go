package operations

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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

func TestPreReservationMigrationClearsPhantomReservationOnLiveDatabase(t *testing.T) {
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

	var productID, variantID, warehouseID, orderID int64
	var sku string
	if err := pool.QueryRow(ctx, `
		INSERT INTO products(name, slug, status, category_id)
		SELECT 'CI legacy reservation product', $1, 'draft', id
		FROM categories WHERE slug='accessories' RETURNING id
	`, "ci-legacy-reservation-"+suffix).Scan(&productID); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO product_variants(product_id, label, base_price_minor)
		VALUES ($1, 'CI legacy reservation variant', 10000)
		RETURNING id, sku
	`, productID).Scan(&variantID, &sku); err != nil {
		t.Fatalf("seed variant: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO warehouses(saby_id, name, city, address)
		VALUES ($1, 'CI legacy reservation warehouse', 'CI', 'CI') RETURNING id
	`, "ci-legacy-reservation-"+suffix).Scan(&warehouseID); err != nil {
		t.Fatalf("seed warehouse: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO inventory(warehouse_id, variant_id, available_qty, reserved_qty)
		VALUES ($1, $2, 5, 0)
	`, warehouseID, variantID); err != nil {
		t.Fatalf("seed inventory: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO orders(
			order_number, customer_name, phone, email, delivery_method,
			delivery_fee, subtotal, total, status, created_at
		) VALUES (
			$1, 'CI historical reservation', '+70000000000', 'ci@example.invalid',
			'pickup', 0, 20000, 20000, 'new', TIMESTAMPTZ '2026-08-01 12:00:00+00'
		) RETURNING id
	`, "CI-LEGACY-RESERVE-"+suffix).Scan(&orderID); err != nil {
		t.Fatalf("seed historical order: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM orders WHERE id=$1", orderID)
		_, _ = pool.Exec(ctx, "DELETE FROM inventory WHERE warehouse_id=$1 AND variant_id=$2", warehouseID, variantID)
		_, _ = pool.Exec(ctx, "DELETE FROM products WHERE id=$1", productID)
		_, _ = pool.Exec(ctx, "DELETE FROM warehouses WHERE id=$1", warehouseID)
	}()
	if _, err := pool.Exec(ctx, `
		INSERT INTO order_items(
			order_id, product_id, variant_id, sku, product_name,
			unit_price, quantity, reserved_qty
		) VALUES ($1, $2, $3, $4, 'CI historical reservation line', 10000, 2, 2)
	`, orderID, productID, variantID, sku); err != nil {
		t.Fatalf("seed phantom reservation: %v", err)
	}

	probe := NewProbe(pool)
	snapshot, err := probe.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !containsCheck(snapshot.Checks, "reservation_ledger_mismatch") {
		t.Fatalf("phantom historical reservation was not detected: %#v", snapshot)
	}

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	migrationPath := filepath.Clean(filepath.Join(
		filepath.Dir(currentFile), "../../../timeweb/migrations/087_reconcile_pre_reservation_orders.sql",
	))
	migrationSQL, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := pool.Exec(ctx, string(migrationSQL)); err != nil {
		t.Fatalf("apply migration repair: %v", err)
	}

	var reservedQty, inventoryReserved int
	var released bool
	if err := pool.QueryRow(ctx, `
		SELECT item.reserved_qty, purchase.stock_released_at IS NOT NULL
		FROM order_items item
		JOIN orders purchase ON purchase.id=item.order_id
		WHERE purchase.id=$1
	`, orderID).Scan(&reservedQty, &released); err != nil {
		t.Fatalf("read repaired order: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT reserved_qty FROM inventory WHERE warehouse_id=$1 AND variant_id=$2
	`, warehouseID, variantID).Scan(&inventoryReserved); err != nil {
		t.Fatalf("read inventory after repair: %v", err)
	}
	if reservedQty != 0 || !released || inventoryReserved != 0 {
		t.Fatalf(
			"unsafe historical repair: item_reserved=%d released=%v inventory_reserved=%d",
			reservedQty, released, inventoryReserved,
		)
	}

	snapshot, err = probe.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if containsCheck(snapshot.Checks, "reservation_ledger_mismatch") {
		t.Fatalf("historical repair left reservation mismatch: %#v", snapshot)
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
