package saby

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStagingStockFreshnessOnDatabase(t *testing.T) {
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
	var name string
	if err = pool.QueryRow(ctx, "SELECT current_database()").Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "ficusin_staging" && !strings.HasPrefix(name, "ficusin_staging_") {
		if _, err := NewStagingStockRefresher(ctx, pool); err == nil {
			t.Fatal("non-staging database accepted")
		}
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `CREATE TEMP TABLE inventory(id INT PRIMARY KEY,synced_at TIMESTAMPTZ);INSERT INTO inventory VALUES(1,NOW()-INTERVAL '3 hours'),(2,TIMESTAMPTZ '1970-01-01'),(3,NOW()-INTERVAL '5 minutes')`); err != nil {
		t.Fatal(err)
	}
	tag, err := tx.Exec(ctx, StagingStockQuery)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("freshness update=%d %v", tag.RowsAffected(), err)
	}
	var oldVoided, recentUnchanged bool
	if err = tx.QueryRow(ctx, `SELECT (SELECT synced_at=TIMESTAMPTZ '1970-01-01' FROM inventory WHERE id=2),(SELECT synced_at=NOW()-INTERVAL '5 minutes' FROM inventory WHERE id=3)`).Scan(&oldVoided, &recentUnchanged); err != nil || !oldVoided || !recentUnchanged {
		t.Fatalf("changed void/recent stock: %v %v %v", oldVoided, recentUnchanged, err)
	}
}
