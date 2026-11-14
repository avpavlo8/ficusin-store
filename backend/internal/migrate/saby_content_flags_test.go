package migrate

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRetiredSabyFlagsPreserveStockAndOriginalArraysOnDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	raw, e := os.ReadFile("../../../timeweb/migrations/20261113_zzzzzzzzzz_retire_saby_content_flags.sql")
	if e != nil {
		t.Fatal(e)
	}
	tx, e := pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	exec := func(sql string) {
		t.Helper()
		if _, e := tx.Exec(ctx, sql); e != nil {
			t.Fatal(e)
		}
	}
	schema := fmt.Sprintf("retired_saby_flags_%d", time.Now().UnixNano())
	archive := schema + "_archive"
	exec("CREATE SCHEMA " + schema + "; SET LOCAL search_path=" + schema)
	exec(`CREATE TABLE products(id BIGINT PRIMARY KEY,saby_id TEXT,saby_fields TEXT[] NOT NULL,name TEXT,description TEXT,price_minor BIGINT,updated_at TIMESTAMPTZ DEFAULT NOW());
INSERT INTO products(id,saby_id,saby_fields,name,description,price_minor) VALUES
 (1,NULL,ARRAY['name','description','photo','price'],'Unlinked site name','Site description',149099),
 (2,'linked',ARRAY['stock','photo'],'Linked site name','Linked description',299099),
 (3,'stock-only',ARRAY['stock'],'Stock only','Stock description',333333),
 (4,NULL,ARRAY[]::TEXT[],'No sync','No sync description',444444),
 (5,NULL,ARRAY['stock','stock','obsolete'],'Duplicate stock','Duplicate description',555555),
 (6,NULL,ARRAY[NULL,'photo']::TEXT[],'Null flag','Null description',666666);
CREATE TABLE product_media(id BIGINT PRIMARY KEY,product_id BIGINT,object_key TEXT,sort_order INT,is_primary BOOLEAN);
INSERT INTO product_media VALUES(1,1,'site-cover',2,TRUE),(2,2,'other-cover',3,FALSE);
CREATE TEMP TABLE saby_flags_before AS SELECT id,saby_fields,to_jsonb(p)-'saby_fields' unchanged FROM products p;
CREATE TEMP TABLE saby_media_before AS SELECT to_jsonb(m) unchanged FROM product_media m;`)
	exec(strings.ReplaceAll(string(raw), "legacy_archive", archive))
	var n int
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM products p JOIN saby_flags_before b USING(id) WHERE (COALESCE('stock'=ANY(p.saby_fields),FALSE) IS DISTINCT FROM COALESCE('stock'=ANY(b.saby_fields),FALSE)) OR to_jsonb(p)-'saby_fields' IS DISTINCT FROM b.unchanged OR p.saby_fields IS DISTINCT FROM CASE WHEN COALESCE('stock'=ANY(b.saby_fields),FALSE) THEN ARRAY['stock']::TEXT[] ELSE ARRAY[]::TEXT[] END`).Scan(&n); e != nil || n != 0 {
		t.Fatalf("stock/content/identity/timestamps changed: %d %v", n, e)
	}
	if e = tx.QueryRow(ctx, "SELECT count(*) FROM "+archive+`.product_saby_content_flags a JOIN saby_flags_before b ON b.id=a.product_id WHERE a.saby_fields IS NOT DISTINCT FROM b.saby_fields AND a.product_updated_at=(b.unchanged->>'updated_at')::timestamptz`).Scan(&n); e != nil || n != 4 {
		t.Fatalf("original changed arrays not preserved: %d %v", n, e)
	}
	if e = tx.QueryRow(ctx, "SELECT count(*) FROM "+archive+`.product_saby_content_flags WHERE product_id IN(3,4)`).Scan(&n); e != nil || n != 0 {
		t.Fatalf("unchanged rows unnecessarily archived: %d %v", n, e)
	}
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM product_media m FULL JOIN saby_media_before b ON to_jsonb(m)=b.unchanged WHERE m.id IS NULL OR b.unchanged IS NULL`).Scan(&n); e != nil || n != 0 {
		t.Fatalf("gallery changed: %d %v", n, e)
	}
}
