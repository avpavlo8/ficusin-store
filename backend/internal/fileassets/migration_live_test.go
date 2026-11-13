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

func TestPrivateFileMigrationLocalDatabase(t *testing.T) {
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
	defer admin.Close()
	schema := fmt.Sprintf("private_file_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
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
