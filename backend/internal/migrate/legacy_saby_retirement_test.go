package migrate

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestLegacySabyExporterRetirementOnDatabase(t *testing.T) {
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
	raw, err := os.ReadFile("../../../timeweb/migrations/20261113_zzzzzz_retire_legacy_saby_export.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"empty", "idle", "synced", "error", "queued", "checking", "verifying", "unknown", "leased", "error-attempted"} {
		t.Run(state, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, err := tx.Exec(ctx, sql, args...); err != nil {
					t.Fatal(err)
				}
			}
			active := fmt.Sprintf("retire_saby_%d", time.Now().UnixNano())
			archive := active + "_archive"
			exec("CREATE SCHEMA " + active + "; SET LOCAL search_path=" + active)
			exec(`CREATE TABLE product_variants(id BIGINT PRIMARY KEY);INSERT INTO product_variants VALUES(1);
CREATE TABLE customers(id BIGINT PRIMARY KEY);INSERT INTO customers VALUES(1);
CREATE TABLE catalog_saby_exports(variant_id BIGINT PRIMARY KEY REFERENCES product_variants(id) ON DELETE CASCADE,actor_id BIGINT REFERENCES customers(id),state TEXT,payload BYTEA,attempted_at TIMESTAMPTZ,remote_id TEXT,lease_until TIMESTAMPTZ);
CREATE FUNCTION protect_exported_catalogue_variant() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN OLD; END $$;
CREATE TRIGGER protect_exported_catalogue_variant BEFORE DELETE ON product_variants FOR EACH ROW EXECUTE FUNCTION protect_exported_catalogue_variant();
CREATE TABLE catalog_editor_publications(variant_id BIGINT PRIMARY KEY,progress JSONB);INSERT INTO catalog_editor_publications VALUES(1,'{"ambiguous":true,"stage":"verify"}');`)
			payload := []byte{0, 255, 128, 1}
			if state != "empty" {
				s := state
				if s == "error-attempted" {
					s = "error"
				}
				if s == "leased" {
					s = "idle"
				}
				exec(`INSERT INTO catalog_saby_exports VALUES(1,1,$1,$2,CASE WHEN $1='synced' THEN NOW() END,CASE WHEN $1='synced' THEN 'remote-1' ELSE '' END,NULL)`, s, payload)
				if state == "leased" {
					exec("UPDATE catalog_saby_exports SET lease_until=NOW()+INTERVAL '1 minute'")
				}
				if state == "error-attempted" {
					exec("UPDATE catalog_saby_exports SET attempted_at=NOW()")
				}
			}
			sql := strings.ReplaceAll(strings.ReplaceAll(string(raw), "legacy_archive", archive), "public", active)
			exec("SAVEPOINT retirement")
			_, err = tx.Exec(ctx, sql)
			if state == "queued" || state == "checking" || state == "verifying" || state == "unknown" || state == "leased" || state == "error-attempted" {
				if err == nil {
					t.Fatal("retired an unresolved export")
				}
				exec("ROLLBACK TO SAVEPOINT retirement")
				var got []byte
				if err := tx.QueryRow(ctx, "SELECT payload FROM catalog_saby_exports WHERE variant_id=1").Scan(&got); err != nil || !bytes.Equal(got, payload) {
					t.Fatalf("rollback lost source bytes: %x %v", got, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var activeExists bool
			if err := tx.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", active+".catalog_saby_exports").Scan(&activeExists); err != nil || activeExists {
				t.Fatalf("old active queue remains: %v %v", activeExists, err)
			}
			// Cached statements in an older instance can still address the moved
			// relation by OID. The archive must reject enqueue/claim writes too.
			exec("SAVEPOINT archived_insert")
			if _, err := tx.Exec(ctx, "INSERT INTO "+archive+".catalog_saby_exports(variant_id,state) VALUES(2,'idle')"); err == nil {
				t.Fatal("retired exporter accepted a new enqueue")
			}
			exec("ROLLBACK TO SAVEPOINT archived_insert")
			if state != "empty" {
				exec("SAVEPOINT archived_update")
				if _, err := tx.Exec(ctx, "UPDATE "+archive+".catalog_saby_exports SET state='checking' WHERE variant_id=1"); err == nil {
					t.Fatal("retired exporter accepted a claim/update")
				}
				exec("ROLLBACK TO SAVEPOINT archived_update")
			}
			exec("DELETE FROM customers WHERE id=1")
			if state == "synced" {
				exec("SAVEPOINT delete_exported")
				if _, err := tx.Exec(ctx, "DELETE FROM product_variants WHERE id=1"); err == nil {
					t.Fatal("exported identity deleted")
				}
				exec("ROLLBACK TO SAVEPOINT delete_exported")
			} else {
				exec("DELETE FROM product_variants WHERE id=1")
			}
			if state != "empty" {
				var got []byte
				if err := tx.QueryRow(ctx, "SELECT payload FROM "+archive+".catalog_saby_exports WHERE variant_id=1").Scan(&got); err != nil || !bytes.Equal(got, payload) {
					t.Fatalf("archive lost bytes: %x %v", got, err)
				}
			}
			var receipt string
			if err := tx.QueryRow(ctx, "SELECT progress->>'stage' FROM catalog_editor_publications WHERE variant_id=1").Scan(&receipt); err != nil || receipt != "verify" {
				t.Fatalf("changed current publisher receipt: %q %v", receipt, err)
			}
		})
	}
}
