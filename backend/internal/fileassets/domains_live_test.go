package fileassets_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/auth"
	"github.com/avpavlo8/ficusin-store/backend/internal/fileassets"
	"github.com/avpavlo8/ficusin-store/backend/internal/procurement"
	"github.com/avpavlo8/ficusin-store/backend/internal/reviews"
	"github.com/avpavlo8/ficusin-store/backend/internal/testsupport"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPrivateFileDomainsLocalDatabase(t *testing.T) {
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
	control, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	schema := fmt.Sprintf("private_domains_%d", time.Now().UnixNano())
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
	_, err = pool.Exec(ctx, `CREATE TABLE customers(id BIGINT PRIMARY KEY,avatar_image BYTEA,avatar_object_key TEXT,avatar_mime TEXT,avatar_updated_at TIMESTAMPTZ,updated_at TIMESTAMPTZ,is_active BOOLEAN DEFAULT TRUE);
 CREATE TABLE marketplace_returns(id BIGINT PRIMARY KEY,condition TEXT,discount_percent INTEGER,deleted_at TIMESTAMPTZ);
 CREATE TABLE marketplace_return_photos(id BIGINT PRIMARY KEY,marketplace_return_id BIGINT,content_type TEXT,image BYTEA,object_key TEXT,deleted_at TIMESTAMPTZ);
 CREATE TABLE procurement_documents(id BIGINT PRIMARY KEY,content BYTEA,object_key TEXT);
 CREATE TABLE product_reviews(id BIGINT PRIMARY KEY,status TEXT);
 CREATE TABLE product_review_photos(id BIGINT PRIMARY KEY,review_id BIGINT,content_type TEXT,image BYTEA,object_key TEXT,storage_scope TEXT DEFAULT 'legacy-public');
 INSERT INTO customers(id)VALUES(1);INSERT INTO marketplace_returns VALUES(1,'restoring',50,NULL);INSERT INTO product_reviews VALUES(1,'published')`)
	if err != nil {
		t.Fatal(err)
	}
	private := testsupport.NewFileStorage()
	avatar := auth.NewService(pool, 30, nil).WithFileStorage(private)
	if err = avatar.SaveAvatar(ctx, 1, []byte("new-avatar"), "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	var dbBytes []byte
	var avatarKey string
	pool.QueryRow(ctx, `SELECT avatar_image,avatar_object_key FROM customers WHERE id=1`).Scan(&dbBytes, &avatarKey)
	if dbBytes != nil || avatarKey == "" {
		t.Fatal("new avatar retained DB bytes")
	}
	data, _, err := avatar.Avatar(ctx, 1)
	if err != nil || string(data) != "new-avatar" {
		t.Fatal(err)
	}
	if _, _, err = auth.NewService(pool, 30, nil).Avatar(ctx, 1); !errors.Is(err, fileassets.ErrUnavailable) {
		t.Fatal("private outage fell back", err)
	}
	if err = avatar.DeleteAvatar(ctx, 1); err != nil {
		t.Fatal(err)
	}
	key, err := fileassets.Save(ctx, private, "returns/1", "image/jpeg", []byte("return-private"))
	if err != nil {
		t.Fatal(err)
	}
	pool.Exec(ctx, `INSERT INTO marketplace_return_photos VALUES(1,1,'image/jpeg',NULL,$1,NULL)`, key)
	repo := admin.NewPostgresRepository(pool).WithFileStorage(private)
	_, data, err = repo.ReturnPhoto(ctx, 1)
	if err != nil || string(data) != "return-private" {
		t.Fatal(err)
	}
	_, data, err = repo.DiscountPlantPhoto(ctx, 1)
	if err != nil || string(data) != "return-private" {
		t.Fatal("approved discount photo unavailable", err)
	}
	key, err = fileassets.Save(ctx, private, "procurement/1", "application/pdf", []byte("private-pdf"))
	if err != nil {
		t.Fatal(err)
	}
	pool.Exec(ctx, `INSERT INTO procurement_documents VALUES(2,NULL,$1)`, key)
	data, err = procurement.NewPostgresStore(pool).WithFileStorage(private).DocumentPDF(ctx, 2)
	if err != nil || string(data) != "private-pdf" {
		t.Fatal(err)
	}
	private.Put(ctx, "private-review", []byte("private-bucket"), "image/jpeg")
	pool.Exec(ctx, `INSERT INTO product_review_photos VALUES(2,1,'image/jpeg',NULL,'private-review','private')`)
	store := reviews.NewStore(pool, private)
	_, data, err = store.Photo(ctx, 2)
	if err != nil || string(data) != "private-bucket" {
		t.Fatal("private review inaccessible", err)
	}
	if _, _, err = reviews.NewStore(pool).Photo(ctx, 2); !errors.Is(err, fileassets.ErrUnavailable) {
		t.Fatal("review read without private storage", err)
	}
	pool.Exec(ctx, `UPDATE product_reviews SET status='hidden' WHERE id=1`)
	if _, _, err = store.Photo(ctx, 2); err == nil {
		t.Fatal("hidden private review became public")
	}
}
