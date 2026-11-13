package payment

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAutomaticFiscalMigrationScopesLegacyTestsOnLiveDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	// Minimal pre-migration schema in its own transactional namespace. The
	// production tables, functions, triggers and migration ledger stay intact.
	schema := fmt.Sprintf("fiscal_migration_%d", time.Now().UnixNano())
	exec(`CREATE SCHEMA ` + schema)
	exec(`SET LOCAL search_path TO ` + schema + `,public`)
	exec(`CREATE TABLE orders(id bigint PRIMARY KEY,status text);
 CREATE TABLE shipment_offers(id bigint PRIMARY KEY,status text);
 CREATE TABLE payments(id bigint PRIMARY KEY,order_id bigint,shipment_offer_id bigint,
  status text,provider_payment_id text,receipt_status text,request_payload jsonb,created_at timestamptz);
 CREATE TABLE payment_settlement_receipts(payment_id bigint PRIMARY KEY,actor_id bigint NOT NULL);
 INSERT INTO orders VALUES(1,'cancelled'),(2,'completed');
 INSERT INTO payments(id,order_id,status,provider_payment_id,receipt_status,created_at) VALUES
 (1,1,'paid','legacy-test-1','','2026-08-15 12:00:00+03'),
 (2,1,'paid','july','','2026-07-31 12:00:00+03'),
 (3,1,'paid','september','','2026-09-01 12:00:00+03'),
 (4,2,'paid','completed','','2026-08-15 12:00:00+03'),
 (5,1,'pending','pending','','2026-08-15 12:00:00+03'),
 (6,1,'paid','has-snapshot','','2026-08-15 12:00:00+03'),
 (7,1,'paid','has-income','succeeded','2026-08-15 12:00:00+03'),
 (8,1,'paid','','','2026-08-15 12:00:00+03'),
 (9,1,'paid','has-settlement','','2026-08-15 12:00:00+03');
 UPDATE payments SET request_payload='{}' WHERE id=6;
 INSERT INTO payment_settlement_receipts VALUES(9,10)`)
	migration, err := os.ReadFile("../../../timeweb/migrations/20261113_automatic_settlement_receipts.sql")
	if err != nil {
		t.Fatal(err)
	}
	exec(string(migration))
	var count int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM payments WHERE provider_test=true AND provider_test_source='owner_confirmed_august_2026'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("backfill classified unrelated payments: %d %v", count, err)
	}
	var tagged bool
	var status string
	if err := tx.QueryRow(ctx, `SELECT provider_test,status FROM payments WHERE id=1`).Scan(&tagged, &status); err != nil || !tagged || status != "paid" {
		t.Fatalf("legacy fact/history was lost: %v", err)
	}
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM payments WHERE settlement_requested_at IS NOT NULL`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("migration enqueued historical completions: %d %v", count, err)
	}
	var actor int64
	var source string
	if err := tx.QueryRow(ctx, `SELECT actor_id,source FROM payment_settlement_receipts WHERE payment_id=9`).Scan(&actor, &source); err != nil || actor != 10 || source != "owner" {
		t.Fatalf("existing owner audit was changed: %v", err)
	}
}
