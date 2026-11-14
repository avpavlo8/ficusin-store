package fileassets

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPrivateFileCutoverLocalDatabase(t *testing.T) {
	ctx := context.Background()
	pool := migrationTestPool(t)
	schema := fmt.Sprintf("file_archive_%d", time.Now().UnixNano())
	// The migration uses a detached archive schema. Substitute only that schema's
	// name so independent domain tests cannot alter another test's archive.
	ddl, err := os.ReadFile("../../../timeweb/migrations/20261114_zzzzz_private_file_cutover.sql")
	if err != nil {
		t.Fatal(err)
	}
	ddl = []byte(strings.ReplaceAll(string(ddl), "legacy_archive", schema))
	t.Cleanup(func() { pool.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE") })
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE customers(id BIGINT PRIMARY KEY,avatar_object_key TEXT,avatar_image BYTEA,avatar_mime TEXT);
 CREATE TABLE marketplace_return_photos(id BIGINT PRIMARY KEY,object_key TEXT,image BYTEA,content_type TEXT);
 CREATE TABLE procurement_documents(id BIGINT PRIMARY KEY,object_key TEXT,content BYTEA,content_type TEXT);
 CREATE TABLE support_messages(id BIGINT PRIMARY KEY,object_key TEXT,image_data BYTEA,image_type TEXT,attachment_size INTEGER);
 CREATE TABLE product_review_photos(id BIGINT PRIMARY KEY,object_key TEXT,image BYTEA,content_type TEXT,storage_scope TEXT NOT NULL DEFAULT 'legacy-public');
 INSERT INTO customers VALUES(1,NULL,decode('00ff0102','hex'),'image/webp'),(2,NULL,NULL,NULL);
 INSERT INTO marketplace_return_photos VALUES(1,'return','return-bytes','image/png');
 INSERT INTO procurement_documents VALUES(1,'pdf','pdf-bytes','application/pdf');
 INSERT INTO support_messages VALUES(1,'chat','chat-bytes','application/pdf',10);
 INSERT INTO product_review_photos VALUES(1,'review','review-bytes','image/jpeg','legacy-public')`)
	apply := func() error {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, string(ddl)); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if err = apply(); err == nil {
		t.Fatal("unkeyed payload did not block cutover")
	}
	var present bool
	if err = pool.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", schema+".file_binary_payloads").Scan(&present); err != nil || present {
		t.Fatal("failed cutover left archive", err)
	}
	exec(`UPDATE customers SET avatar_object_key='avatar' WHERE id=1`)
	if err = apply(); err == nil {
		t.Fatal("legacy-public object did not block cutover")
	}
	exec(`UPDATE product_review_photos SET storage_scope='private'`)
	if err = apply(); err != nil {
		t.Fatal(err)
	}
	var copied []byte
	var objectKey, originalMime string
	if err = pool.QueryRow(ctx, "SELECT payload,destination_key,original_metadata->>'avatar_mime' FROM "+schema+".file_binary_payloads WHERE kind='avatars' AND source_id=1").Scan(&copied, &objectKey, &originalMime); err != nil || !bytes.Equal(copied, []byte{0, 255, 1, 2}) || objectKey != "avatar" || originalMime != "image/webp" {
		t.Fatal("archive changed binary or metadata", err)
	}
	var count int
	if err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM "+schema+".file_binary_payloads").Scan(&count); err != nil || count != 5 {
		t.Fatal("archive omitted a file kind", count, err)
	}
	for _, source := range migrationSources {
		var remaining int
		if err = pool.QueryRow(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s IS NOT NULL", source.table, source.data)).Scan(&remaining); err != nil || remaining != 0 {
			t.Fatal(source.kind, "runtime retains binary", err)
		}
		if _, err = pool.Exec(ctx, fmt.Sprintf("UPDATE %s SET %s='old-writer' WHERE id=1", source.table, source.data)); err == nil {
			t.Fatal(source.kind, "old writer resurrected bytea")
		}
	}
	if _, err = pool.Exec(ctx, `UPDATE product_review_photos SET storage_scope='legacy-public' WHERE id=1`); err == nil {
		t.Fatal("public bucket dependency resurrected")
	}
	// A current S3-only writer can still replace its key without changing archive.
	exec(`UPDATE customers SET avatar_object_key='replacement' WHERE id=1`)
}
