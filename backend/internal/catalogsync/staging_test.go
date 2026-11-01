package catalogsync

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStagingProviderIsolationAndIdempotence(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	host := cfg.ConnConfig.Host
	if host != "localhost" && host != "127.0.0.1" && host != "::1" && !strings.HasPrefix(host, "/") {
		t.Skip("database creation is restricted to a local test server")
	}
	ctx := context.Background()
	root, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if !strings.HasPrefix(cfg.ConnConfig.Database, "ficusin_staging") {
		if _, err := NewStagingProvider(ctx, root); err == nil {
			t.Fatal("simulation accepted a non-staging database")
		}
	}
	name := fmt.Sprintf("ficusin_staging_export_%d", time.Now().UnixNano())
	if _, err = root.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := root.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
	}()
	cfg = cfg.Copy()
	cfg.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	_, err = pool.Exec(ctx, `CREATE TABLE saby_nomenclature(saby_id text PRIMARY KEY,code text,article text,name text,description text,price_minor bigint,balance bigint)`)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := NewStagingProvider(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := Snapshot{VariantID: 1, SKU: "875", Code: "8e46fe11-b919-51a8-ad0f-fd2214254782", Name: "Test plant", PotCM: 11.3, HeightCM: 40, PriceMinor: 149099}
	remote, err := provider.Lookup(ctx, snapshot)
	if err != nil || remote != nil {
		t.Fatalf("initial lookup: %v %v", remote, err)
	}
	for range 2 {
		if err := provider.Write(ctx, snapshot, true); err != nil {
			t.Fatal(err)
		}
	}
	remote, err = provider.Lookup(ctx, snapshot)
	if err != nil || !snapshot.Matches(remote) {
		t.Fatalf("roundtrip: %v %v", remote, err)
	}
	originalID := remote.ID
	snapshot.SabyID = originalID
	snapshot.PotCM = 12
	snapshot.HeightCM = 45
	snapshot.PriceMinor = 159099
	if err := provider.Write(ctx, snapshot, false); err != nil {
		t.Fatal(err)
	}
	remote, err = provider.Lookup(ctx, snapshot)
	if err != nil || remote.ID != originalID || remote.PriceMinor != 149099 || remote.HeightCM != 45 || remote.PotCM != 12 {
		t.Fatalf("price omission/update: %v %v", remote, err)
	}
	if err := provider.Write(ctx, snapshot, true); err != nil {
		t.Fatal(err)
	}
	remote, err = provider.Lookup(ctx, snapshot)
	if err != nil || !snapshot.Matches(remote) || remote.ID != originalID {
		t.Fatalf("update: %v %v", remote, err)
	}
	var count, balance int
	if err := pool.QueryRow(ctx, `SELECT count(*),coalesce(sum(balance),0) FROM saby_nomenclature`).Scan(&count, &balance); err != nil || count != 1 || balance != 0 {
		t.Fatalf("duplicates/stock: count=%d balance=%d err=%v", count, balance, err)
	}
	snapshot.SabyID = "3952"
	if err := provider.Write(ctx, snapshot, true); err == nil {
		t.Fatal("accepted a real Saby identity")
	}
	if _, err := provider.Lookup(ctx, snapshot); err == nil {
		t.Fatal("looked up a real Saby identity")
	}
}
