package catalogeditor

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSabyPreviewModeIsolationOnLiveDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close()
	schema := fmt.Sprintf("saby_preview_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+quoted); e != nil {
		t.Fatal(e)
	}
	defer admin.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE")
	config, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, e := pgxpool.NewWithConfig(ctx, config)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	_, e = pool.Exec(ctx, `CREATE TABLE product_variants(id BIGINT PRIMARY KEY,product_id BIGINT,saby_id TEXT);
CREATE TABLE catalog_editor_publications(variant_id BIGINT,channel TEXT,payload JSONB,status TEXT,execution_mode TEXT,message TEXT DEFAULT '',updated_at TIMESTAMPTZ DEFAULT NOW());
CREATE TABLE saby_nomenclature(saby_id TEXT PRIMARY KEY,code TEXT,article TEXT,barcode TEXT,name TEXT,description TEXT,price_minor BIGINT,balance INT,images TEXT[],folder_saby_id TEXT);
CREATE TABLE product_external_ids(product_id BIGINT,variant_id BIGINT,provider TEXT,id_type TEXT,external_id TEXT,source TEXT,UNIQUE(provider,id_type,external_id));
INSERT INTO product_variants VALUES(1,10,NULL),(2,20,NULL);
INSERT INTO catalog_editor_publications(variant_id,channel,payload,status,execution_mode,updated_at) VALUES
(1,'saby','{"name":"Disabled card","barcode":"2000000000001","priceMinor":10000}','prepared','disabled',NOW()-INTERVAL '1 hour'),
(2,'saby','{"name":"Mock card","barcode":"2000000000002","priceMinor":20000}','prepared','mock',NOW());`)
	if e != nil {
		t.Fatal(e)
	}
	if e = step(ctx, pool); e != nil {
		t.Fatal(e)
	}
	if e = step(ctx, pool); e != nil {
		t.Fatal(e)
	}
	var disabled, mock string
	if e = pool.QueryRow(ctx, `SELECT status FROM catalog_editor_publications WHERE variant_id=1`).Scan(&disabled); e != nil {
		t.Fatal(e)
	}
	if e = pool.QueryRow(ctx, `SELECT status FROM catalog_editor_publications WHERE variant_id=2`).Scan(&mock); e != nil {
		t.Fatal(e)
	}
	if disabled != "prepared" || mock != "simulated" {
		t.Fatalf("mode boundary failed: disabled=%s mock=%s", disabled, mock)
	}
	var disabledID *string
	var mockID string
	if e = pool.QueryRow(ctx, `SELECT saby_id FROM product_variants WHERE id=1`).Scan(&disabledID); e != nil {
		t.Fatal(e)
	}
	if e = pool.QueryRow(ctx, `SELECT saby_id FROM product_variants WHERE id=2`).Scan(&mockID); e != nil {
		t.Fatal(e)
	}
	var imports, links int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM saby_nomenclature`).Scan(&imports); e != nil {
		t.Fatal(e)
	}
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM product_external_ids WHERE variant_id=2 AND source='local-preview'`).Scan(&links); e != nil {
		t.Fatal(e)
	}
	if disabledID != nil || mockID != "STG-EDITOR-2000000000002" || imports != 1 || links != 1 {
		t.Fatalf("disabled card mutated canonical data or mock failed: %v/%s/%d/%d", disabledID, mockID, imports, links)
	}
}
