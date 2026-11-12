package fileassets

import (
	"context"
	"encoding/base64"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
	"time"
)

func TestStagingMockStorageDatabase(t *testing.T) {
	dsn := os.Getenv("STAGING_FILE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("STAGING_FILE_TEST_DATABASE_URL not set")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if (cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost") || cfg.ConnConfig.Database != "ficusin_staging_file_mock_test" {
		t.Fatal("dedicated local staging mock DB required")
	}
	ctx := context.Background()
	control, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	schema := fmt.Sprintf("staging_files_%d", time.Now().UnixNano())
	if _, err = control.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer control.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, name := range []string{"20261112_staging_private_file_mock.sql", "20261112_staging_public_file_mock.sql"} {
		sql, err := os.ReadFile("../../../timeweb/migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, string(sql)); err != nil {
			t.Fatal(err)
		}
	}
	private, err := NewStagingPrivateStorage(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	key, err := Save(ctx, private, "chat/7", "application/pdf", []byte("private-test-pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if _, public := any(private).(interface{ PublicURL(string) string }); public {
		t.Fatal("private mock exposes public URL")
	}
	reopened, err := NewStagingPrivateStorage(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	bytes, err := reopened.Get(ctx, key)
	if err != nil || string(bytes) != "private-test-pdf" {
		t.Fatal("staging restart lost bytes", err)
	}
	public, err := NewStagingPublicStorage(ctx, pool, "https://isolated-staging.example.test")
	if err != nil {
		t.Fatal(err)
	}
	imageKey, err := Save(ctx, public, "products/64", "image/jpeg", []byte("test-jpeg"))
	if err != nil {
		t.Fatal(err)
	}
	url := public.PublicURL(imageKey)
	if !strings.HasPrefix(url, "https://isolated-staging.example.test/api/v1/staging/media/") {
		t.Fatal(url)
	}
	token := strings.TrimPrefix(url, "https://isolated-staging.example.test/api/v1/staging/media/")
	decoded, _ := base64.RawURLEncoding.DecodeString(token)
	if string(decoded) != imageKey {
		t.Fatal("wrong public key")
	}
	mime, bytes, err := public.ReadPublicObject(ctx, imageKey)
	if err != nil || mime != "image/jpeg" || string(bytes) != "test-jpeg" {
		t.Fatal("public roundtrip", err)
	}
	if _, err = public.Get(ctx, key); err == nil {
		t.Fatal("private namespace was read through public store")
	}
	private.Put(ctx, "products/secret", []byte("still-private"), "text/plain")
	if _, err = public.Get(ctx, "products/secret"); err == nil {
		t.Fatal("private table leaked through overlapping key prefix")
	}
	if public.PublicURL("chat/7/private") != "" {
		t.Fatal("public URL for private attachment")
	}
	if err = public.Put(ctx, "products/document", []byte("pdf"), "application/pdf"); err == nil {
		t.Fatal("public mock accepted private document MIME")
	}
	if err = private.Put(ctx, "../escape", []byte("x"), "text/plain"); err == nil {
		t.Fatal("invalid key accepted")
	}
	pool.Exec(ctx, `UPDATE private_file_mock_objects SET body='corrupt' WHERE object_key=$1`, key)
	if _, err = private.Get(ctx, key); err == nil {
		t.Fatal("corrupt persisted mock object accepted")
	}
	if err = public.Delete(ctx, imageKey); err != nil {
		t.Fatal(err)
	}
}
