package fileassets

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
	"time"
)

func migrationTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if (cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost") || !strings.HasPrefix(cfg.ConnConfig.Database, "ficusin_local_") {
		t.Fatal("isolated local database required")
	}
	ctx := context.Background()
	admin, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	schema := fmt.Sprintf("private_file_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE") })
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	ddl, err := os.ReadFile("../../../timeweb/migrations/20261114_zzzz_private_file_migration_history.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, string(ddl)); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestPrivateFileMigrationLocalDatabase(t *testing.T) {
	ctx := context.Background()
	pool := migrationTestPool(t)
	var err error
	if _, err = pool.Exec(ctx, `CREATE TABLE customers(id BIGINT PRIMARY KEY,avatar_object_key TEXT,avatar_image BYTEA,avatar_mime TEXT);INSERT INTO customers VALUES(1,NULL,'legacy','image/jpeg')`); err != nil {
		t.Fatal(err)
	}
	storage := &fake{data: map[string][]byte{}}
	dry, err := MigrateLegacy(ctx, pool, nil, MigrationOptions{Kinds: []string{"avatars"}})
	if err != nil || len(dry) != 1 || dry[0].Status != "would-copy" || len(storage.data) != 0 {
		t.Fatal("dry-run had side effects", dry, err)
	}
	copied, err := MigrateLegacy(ctx, pool, storage, MigrationOptions{Apply: true, Kinds: []string{"avatars"}})
	if err != nil || len(copied) != 1 || copied[0].Status != "copied-verified-source-retained" {
		t.Fatal(copied, err)
	}
	var key string
	var data []byte
	if err = pool.QueryRow(ctx, "SELECT avatar_object_key,avatar_image FROM customers WHERE id=1").Scan(&key, &data); err != nil || string(data) != "legacy" || string(storage.data[key]) != "legacy" {
		t.Fatal("migration lost source bytes", err)
	}
	again, err := MigrateLegacy(ctx, pool, storage, MigrationOptions{Apply: true, Kinds: []string{"avatars"}})
	if err != nil || len(again) != 0 || len(storage.data) != 1 {
		t.Fatal("repeat migration duplicated objects", again, err)
	}
	if _, err = pool.Exec(ctx, "INSERT INTO customers VALUES(2,NULL,'another','image/jpeg')"); err != nil {
		t.Fatal(err)
	}
	storage.corrupt = true
	if _, err = MigrateLegacy(ctx, pool, storage, MigrationOptions{Apply: true, Kinds: []string{"avatars"}}); err == nil {
		t.Fatal("corrupt S3 copy accepted")
	}
	var linked bool
	pool.QueryRow(ctx, "SELECT avatar_object_key IS NOT NULL FROM customers WHERE id=2").Scan(&linked)
	if linked {
		t.Fatal("bad object reference committed")
	}
}

// Fault injection runs through real CAS transactions; source and destination
// stores are isolated in memory so no test can contact an external bucket.
type callbackStorage struct {
	Storage
	afterPut func()
	afterGet func()
}

func (s *callbackStorage) Put(ctx context.Context, key string, data []byte, mime string) error {
	if err := s.Storage.Put(ctx, key, data, mime); err != nil {
		return err
	}
	if s.afterPut != nil {
		f := s.afterPut
		s.afterPut = nil
		f()
	}
	return nil
}
func (s *callbackStorage) Get(ctx context.Context, key string) ([]byte, error) {
	data, err := s.Storage.Get(ctx, key)
	if s.afterGet != nil {
		f := s.afterGet
		s.afterGet = nil
		f()
	}
	return data, err
}
func TestLegacyPublicReviewCopyAndVerificationLocalDatabase(t *testing.T) {
	ctx := context.Background()
	pool := migrationTestPool(t)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE product_review_photos(id BIGINT PRIMARY KEY,object_key TEXT,image BYTEA,content_type TEXT,storage_scope TEXT NOT NULL);INSERT INTO product_review_photos VALUES(1,'public/one','stale','image/webp','legacy-public')`)
	source := &fake{data: map[string][]byte{"public/one": []byte("current-source")}}
	target := &fake{data: map[string][]byte{}}
	options := MigrationOptions{Limit: 100, Kinds: []string{"reviews"}}
	dry, err := MigrateLegacyPublicReviews(ctx, pool, source, target, options)
	if err != nil || len(dry) != 1 || len(target.data) != 0 || source.deleted != 0 {
		t.Fatal(dry, err)
	}
	blocked, err := VerifyPrivateReferences(ctx, pool, target, options)
	if err != nil || blocked.Ready || blocked.RemainingLegacy != 1 {
		t.Fatal(blocked, err)
	}
	options.Apply = true
	copied, err := MigrateLegacyPublicReviews(ctx, pool, source, target, options)
	if err != nil || len(copied) != 1 || copied[0].Status != "copied-verified-source-retained" {
		t.Fatal(copied, err)
	}
	var key, scope, oldKey, digest string
	var original []byte
	if err = pool.QueryRow(ctx, `SELECT object_key,storage_scope,image FROM product_review_photos WHERE id=1`).Scan(&key, &scope, &original); err != nil {
		t.Fatal(err)
	}
	if scope != "private" || string(target.data[key]) != "current-source" || string(original) != "stale" || source.deleted != 0 {
		t.Fatal("source data or original public object changed")
	}
	if err = pool.QueryRow(ctx, `SELECT source_key,sha256 FROM file_asset_migration_history WHERE source_id=1`).Scan(&oldKey, &digest); err != nil || oldKey != "public/one" || digest != copied[0].SHA256 {
		t.Fatal("missing migration receipt", err)
	}
	again, err := MigrateLegacyPublicReviews(ctx, pool, source, target, options)
	if err != nil || len(again) != 0 || len(target.data) != 1 {
		t.Fatal(again, err)
	}
	options.Apply = false
	verified, err := VerifyPrivateReferences(ctx, pool, target, options)
	if err != nil || !verified.Complete || verified.Ready || verified.Files[0].Status != "checksum-verified" {
		t.Fatal(verified, err)
	}
	target.data[key] = []byte("corrupt")
	verified, err = VerifyPrivateReferences(ctx, pool, target, options)
	if err != nil || verified.Ready || verified.Failed != 1 || verified.Files[0].Status != "checksum-mismatch" {
		t.Fatal(verified, err)
	}
	target.data[key] = []byte("current-source")
	exec(`INSERT INTO product_review_photos VALUES(2,'new',NULL,'image/jpeg','private')`)
	target.data["new"] = []byte("new-upload")
	options.Limit = 1
	verified, err = VerifyPrivateReferences(ctx, pool, target, options)
	if err != nil || verified.Complete || verified.Ready || verified.Referenced != 2 {
		t.Fatal("partial sample marked ready", verified, err)
	}
	options.Limit = 100
	hook := &callbackStorage{Storage: target, afterGet: func() { exec(`UPDATE product_review_photos SET object_key='replacement' WHERE id=1`) }}
	verified, err = VerifyPrivateReferences(ctx, pool, hook, options)
	if err != nil || verified.Ready || verified.Files[0].Status != "changed-concurrently-recheck-required" {
		t.Fatal(verified, err)
	}
	exec(`INSERT INTO product_review_photos VALUES(3,'public/one',NULL,'image/jpeg','legacy-public')`)
	hook = &callbackStorage{Storage: target, afterPut: func() {
		exec(`UPDATE product_review_photos SET object_key='newer-reference',storage_scope='private' WHERE id=3`)
	}}
	options.Apply = true
	skipped, err := MigrateLegacyPublicReviews(ctx, pool, source, hook, options)
	if err != nil || len(skipped) != 1 || skipped[0].Status != "changed-concurrently-skipped" || source.deleted != 0 || target.deleted != 1 {
		t.Fatal(skipped, err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM file_asset_migration_history WHERE source_id=3`).Scan(&count); err != nil || count != 0 {
		t.Fatal("CAS miss left misleading receipt", err)
	}
	exec(`ALTER TABLE file_asset_migration_history ADD CONSTRAINT reject_test_row CHECK (source_id<>4);INSERT INTO product_review_photos VALUES(4,'public/one',NULL,'image/jpeg','legacy-public')`)
	if _, err = MigrateLegacyPublicReviews(ctx, pool, source, target, options); err == nil {
		t.Fatal("history failure did not block reference commit")
	}
	if err = pool.QueryRow(ctx, `SELECT object_key,storage_scope FROM product_review_photos WHERE id=4`).Scan(&key, &scope); err != nil || key != "public/one" || scope != "legacy-public" || source.deleted != 0 {
		t.Fatal("rollback lost original reference", err)
	}
}

func TestArchivedFileDigestLocalDatabase(t *testing.T) {
	ctx := context.Background()
	pool := migrationTestPool(t)
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('legacy_archive.file_binary_payloads') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Skip("requires migrated post-cutover local database")
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	id := time.Now().UnixNano()
	key := fmt.Sprintf("archive-fixture/%d", id)
	exec(`CREATE TABLE customers(id BIGINT PRIMARY KEY,avatar_object_key TEXT,avatar_image BYTEA,avatar_mime TEXT)`)
	exec(`INSERT INTO customers VALUES($1,$2,NULL,'image/jpeg')`, id, key)
	exec(`INSERT INTO legacy_archive.file_binary_payloads(kind,source_id,destination_key,payload,sha256,original_metadata) VALUES('avatars',$1,$2,'original-archived',encode(sha256('original-archived'::BYTEA),'hex'),'{}')`, id, key)
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM legacy_archive.file_binary_payloads WHERE kind='avatars' AND source_id=$1`, id)
	})
	target := &fake{data: map[string][]byte{key: []byte("original-archived")}}
	report, err := VerifyPrivateReferences(ctx, pool, target, MigrationOptions{Kinds: []string{"avatars"}})
	if err != nil || !report.Complete || report.AllKinds || report.Ready || report.Files[0].Status != "checksum-verified" {
		t.Fatal("archived digest not used or partial kinds allowed cutover", report, err)
	}
	target.data[key] = []byte("changed")
	report, err = VerifyPrivateReferences(ctx, pool, target, MigrationOptions{Kinds: []string{"avatars"}})
	if err != nil || report.Failed != 1 || report.Files[0].Status != "checksum-mismatch" {
		t.Fatal(report, err)
	}
	exec(`UPDATE customers SET avatar_object_key='replacement' WHERE id=$1`, id)
	target.data["replacement"] = []byte("new revision")
	report, err = VerifyPrivateReferences(ctx, pool, target, MigrationOptions{Kinds: []string{"avatars"}})
	if err != nil || report.Failed != 0 || report.Files[0].Status != "readable-no-retained-source" {
		t.Fatal("old archive shadowed a newer file", report, err)
	}
}
