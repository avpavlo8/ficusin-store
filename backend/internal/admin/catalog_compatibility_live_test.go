package admin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/migrate"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This test owns an empty, explicitly named local database. It exercises the
// actual before/after migration, including a frozen unknown provider receipt.
func TestCatalogCompatibilityMigrationPostgres(t *testing.T) {
	dsn := os.Getenv("CATALOG_COMPATIBILITY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("CATALOG_COMPATIBILITY_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConnConfig.Host != "127.0.0.1" || (cfg.ConnConfig.Port != 55440 && !(os.Getenv("CI") == "true" && cfg.ConnConfig.Port == 5432)) || cfg.ConnConfig.Database != "ficusin_catalog_compatibility_test" {
		t.Fatal("requires isolated loopback ficusin_catalog_compatibility_test on port 55440")
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`DROP SCHEMA IF EXISTS legacy_archive CASCADE; DROP SCHEMA public CASCADE; CREATE SCHEMA public`)
	const migration = "20261114_zzzz_catalog_plan_compatibility.sql"
	dir := filepath.Join("..", "..", "..", "timeweb", "migrations")
	beforeDir := t.TempDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") || e.Name() == migration {
			continue
		}
		data, e2 := os.ReadFile(filepath.Join(dir, e.Name()))
		if e2 != nil {
			t.Fatal(e2)
		}
		if err = os.WriteFile(filepath.Join(beforeDir, e.Name()), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err = migrate.Apply(ctx, pool, beforeDir); err != nil {
		t.Fatal(err)
	}
	var customer int64
	if err = pool.QueryRow(ctx, `INSERT INTO customers(phone,full_name,consent_at) VALUES('catalog-compatibility-owner','Compatibility test',NOW()) RETURNING id`).Scan(&customer); err != nil {
		t.Fatal(err)
	}
	repo := NewPostgresRepository(pool)
	owner := Actor{CustomerID: customer, Role: RoleOwner}
	var products []Product
	var variants []int64
	for i := 0; i < 5; i++ {
		p, e := repo.CreateProduct(ctx, owner, ProductCreate{Name: "Compatibility test", PriceMinor: 10000, CatalogSection: "plants"})
		if e != nil {
			t.Fatal(e)
		}
		vs, e := repo.ListProductVariants(ctx, p.ID)
		if e != nil {
			t.Fatal(e)
		}
		products = append(products, p)
		variants = append(variants, vs[0].ID)
	}
	configs := []string{
		`{"keep":{"unknown":true},"variants":[{"variantId":1,"wbPrice":"123.45","ozonPrice":"456","marketplace":{"article":" shared ","wb":{"article":null,"category":{"id":"old-category"}},"ozon":{"article":""}}}]}`,
		`{"variants":[{"variantId":2,"priceMode":"auto","wbPrice":"123.45","marketplace":{"article":"unused","wb":{"article":"explicit"},"ozon":{"article":"oz"}}}]}`,
		`{"variants":[{"variantId":3,"marketplace":{"article":"no-price"}}]}`,
		`{"variants":[{"variantId":4,"marketplace":{"article":"must-not-repair","wb":42,"ozon":{"article":""}}}]}`,
		`{"variants":[{"variantId":5,"priceMode":"manual","wbPrice":"100","marketplace":{"wb":{"article":"w"},"ozon":{"article":"o"}}}]}`,
	}
	for i, c := range configs {
		exec(`INSERT INTO catalog_editor_plans(product_id,config) VALUES($1,$2)`, products[i].ID, c)
	}
	exec(`ALTER TABLE legacy_archive.catalog_saby_exports DISABLE TRIGGER reject_retired_saby_export_write`)
	exec(`INSERT INTO legacy_archive.catalog_saby_exports(variant_id,import_code,state,attempted_at,remote_id,payload,payload_hash,claim_token) VALUES
	 ($1,'history-synced','synced',NOW(),'old-saby-id','{"identity":900719925474099312345}','original-hash','original-claim'),
	 ($2,'history-unknown','unknown',NOW(),'','{"identity":"unknown-must-not-replay"}','unknown-hash','unknown-claim'),
	 ($3,'history-idle','idle',NULL,'','{}','',''),
	 (9223372036854775806,'history-orphan','error',NULL,'','{"retainedOrphan":true}','','')`, variants[0], variants[1], variants[2])
	exec(`ALTER TABLE legacy_archive.catalog_saby_exports ENABLE TRIGGER reject_retired_saby_export_write`)
	exec(`INSERT INTO catalog_editor_publications(variant_id,channel,fingerprint,payload,status,execution_mode,progress,pending_payload,pending_fingerprint)
	 VALUES($1,'ozon','immutable-original','{"marketplace":{"article":"frozen-shared"},"channel":{},"barcode":"2000000000008","huge":900719925474099312345}','needs_attention','live',
	 '{"stage":"reconcile","ambiguous":true,"createAttempted":true,"ids":{"taskId":"keep-task"}}',
	 '{"marketplace":{"article":"pending-shared"},"channel":{},"huge":900719925474099312346}','immutable-pending')`, variants[0])
	var frozenBefore, archiveBefore, linksBefore []byte
	const linksSQL = `SELECT jsonb_build_object('variants',(SELECT jsonb_agg(jsonb_build_object('id',id,'sabyId',saby_id) ORDER BY id) FROM product_variants),'external',(SELECT jsonb_agg(to_jsonb(x) ORDER BY variant_id,provider,external_id) FROM product_external_ids x))`
	if err = pool.QueryRow(ctx, linksSQL).Scan(&linksBefore); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT to_jsonb(p) FROM catalog_editor_publications p WHERE variant_id=$1`, variants[0]).Scan(&frozenBefore); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(e) ORDER BY import_code) FROM legacy_archive.catalog_saby_exports e`).Scan(&archiveBefore); err != nil {
		t.Fatal(err)
	}
	if err = migrate.Apply(ctx, pool, dir); err != nil {
		t.Fatal(err)
	}
	assertBool := func(q string, args ...any) {
		t.Helper()
		var b bool
		if e := pool.QueryRow(ctx, q, args...).Scan(&b); e != nil || !b {
			t.Fatalf("invariant failed: %s: %v", q, e)
		}
	}
	assertBool(`SELECT config#>>'{variants,0,marketplace,wb,article}'=' shared ' AND config#>>'{variants,0,marketplace,ozon,article}'='' AND NOT(config#>'{variants,0,marketplace}'?'article') AND config#>>'{variants,0,priceMode}'='manual' AND config#>>'{variants,0,wbPrice}'='123.45' AND config->'keep'='{"unknown":true}' FROM catalog_editor_plans WHERE product_id=$1`, products[0].ID)
	assertBool(`SELECT config#>>'{variants,0,priceMode}'='auto' AND config#>>'{variants,0,marketplace,wb,article}'='explicit' FROM catalog_editor_plans WHERE product_id=$1`, products[1].ID)
	assertBool(`SELECT NOT(config#>'{variants,0}'?'priceMode') AND config#>>'{variants,0,marketplace,wb,article}'='no-price' FROM catalog_editor_plans WHERE product_id=$1`, products[2].ID)
	assertBool(`SELECT config#>'{variants,0,marketplace,wb}'='42'::JSONB AND config#>>'{variants,0,marketplace,article}'='must-not-repair' FROM catalog_editor_plans WHERE product_id=$1`, products[3].ID)
	assertBool(`SELECT count=1 FROM catalog_compatibility_inventory WHERE kind='editable_invalid_shape'`)
	assertBool(`SELECT NOT(catalog_editor_canonical_variant('{"wbPrice":" \t\n\u00a0"}')?'priceMode')`)
	assertBool(`SELECT catalog_editor_canonical_variant('{"marketplace":{"article":"inherit","wb":null,"ozon":{"article":null}}}')#>>'{marketplace,wb,article}'='inherit' AND catalog_editor_canonical_variant('{"marketplace":{"article":"inherit","wb":null,"ozon":{"article":null}}}')#>>'{marketplace,ozon,article}'='inherit'`)
	assertBool(`SELECT count=1 FROM catalog_compatibility_inventory WHERE kind='saby_history_needs_reconciliation'`)
	for _, kind := range []string{"frozen_shared_article", "pending_or_unknown_shared_article", "pending_revision_shared_article"} {
		assertBool(`SELECT count=1 FROM catalog_compatibility_inventory WHERE kind=$1`, kind)
	}
	assertBool(`SELECT to_jsonb(p)=$2::JSONB FROM catalog_editor_publications p WHERE variant_id=$1`, variants[0], string(frozenBefore))
	assertBool(`SELECT jsonb_agg(to_jsonb(e) ORDER BY import_code)=$1::JSONB FROM legacy_archive.catalog_saby_exports e`, string(archiveBefore))
	assertBool(`SELECT COUNT(*)=4 AND bool_and(evidence_hash=encode(sha256(convert_to(evidence::TEXT,'UTF8')),'hex')) FROM catalog_external_operation_history`)
	assertBool(`SELECT before_config=$2::JSONB AND source_hash=encode(sha256(convert_to(before_config::TEXT,'UTF8')),'hex') FROM catalog_plan_format_history WHERE product_id=$1`, products[0].ID, configs[0])
	assertBool(`SELECT bool_and(config=catalog_editor_canonical_plan(config)) FROM catalog_editor_plans`)
	var linksAfter []byte
	if err = pool.QueryRow(ctx, linksSQL).Scan(&linksAfter); err != nil {
		t.Fatal(err)
	}
	if string(linksBefore) != string(linksAfter) {
		t.Fatal("migration changed active external identities")
	}
	// An old source row is no longer required by runtime deletion guards.
	exec(`DELETE FROM legacy_archive.catalog_saby_exports WHERE variant_id=$1`, variants[0])
	if _, err = repo.DeleteDraftProducts(ctx, owner, []int64{products[0].ID}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bulk draft deletion ignored historical identity: %v", err)
	}
	if _, err = pool.Exec(ctx, `DELETE FROM product_variants WHERE id=$1`, variants[0]); err == nil {
		t.Fatal("historical external identity was deletable")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = removeProductVariant(ctx, tx, owner, products[0].ID, variants[0]); err != nil {
		tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	assertBool(`SELECT archived_at IS NOT NULL FROM product_variants WHERE id=$1`, variants[0])
	for _, table := range []string{"catalog_external_operation_history", "catalog_plan_format_history"} {
		if _, err = pool.Exec(ctx, "DELETE FROM "+table); err == nil {
			t.Fatalf("%s evidence deletable", table)
		}
	}
	// The editable writer also canonicalizes new submissions from old clients.
	var plan EditorPlan
	if err = json.Unmarshal([]byte(`{"variants":[{"variantId":0,"wbPrice":"250","marketplace":{"article":"old-client","ozon":{"article":""}}}]}`), &plan); err != nil {
		t.Fatal(err)
	}
	plan.Variants[0].VariantID = variants[4]
	if _, err = repo.SaveEditorPlan(ctx, owner, products[4].ID, plan, false); err != nil {
		t.Fatal(err)
	}
	assertBool(`SELECT config#>>'{variants,0,priceMode}'='manual' AND config#>>'{variants,0,marketplace,wb,article}'='old-client' AND config#>>'{variants,0,marketplace,ozon,article}'='' AND NOT(config#>'{variants,0,marketplace}'?'article') FROM catalog_editor_plans WHERE product_id=$1`, products[4].ID)
	// Re-executing the immutable migration cannot duplicate/corrupt evidence.
	data, err := os.ReadFile(filepath.Join(dir, migration))
	if err != nil {
		t.Fatal(err)
	}
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, string(data)); err != nil {
		tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	assertBool(`SELECT COUNT(*)=4 FROM catalog_external_operation_history`)
}
