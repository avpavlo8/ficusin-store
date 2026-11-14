package settings

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRetiredTelegramSettingOnDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	adminPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer adminPool.Close()
	raw, err := os.ReadFile("../../../timeweb/migrations/20261113_zzzzzzzzz_retire_unused_telegram_setting.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"absent", "0", "1"} {
		t.Run(value, func(t *testing.T) {
			schema := fmt.Sprintf("retired_setting_%d", time.Now().UnixNano())
			archive := schema + "_archive"
			if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
				t.Fatal(err)
			}
			defer adminPool.Exec(ctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE; DROP SCHEMA IF EXISTS "+archive+" CASCADE")
			cfg, err := pgxpool.ParseConfig(dsn)
			if err != nil {
				t.Fatal(err)
			}
			cfg.ConnConfig.RuntimeParams["search_path"] = schema
			pool, err := pgxpool.NewWithConfig(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			must := func(sql string, args ...any) {
				t.Helper()
				if _, err := pool.Exec(ctx, sql, args...); err != nil {
					t.Fatal(err)
				}
			}
			must(`CREATE TABLE settings(key TEXT PRIMARY KEY,value TEXT NOT NULL,updated_at TIMESTAMPTZ NOT NULL);
INSERT INTO settings VALUES('payments.enabled','1','2026-01-02 03:04:05+00'),('unknown.future.preference','original future value','2026-02-03 04:05:06+00')`)
			if value != "absent" {
				must(`INSERT INTO settings VALUES('telegram.enabled',$1,'2026-03-04 05:06:07+00')`, value)
			}
			const activeSnapshot = `SELECT jsonb_agg(to_jsonb(s) ORDER BY key)::text FROM settings s WHERE key<>'telegram.enabled'`
			var before, after string
			if err := pool.QueryRow(ctx, activeSnapshot).Scan(&before); err != nil {
				t.Fatal(err)
			}
			must(strings.ReplaceAll(string(raw), "legacy_archive", archive))
			if err := pool.QueryRow(ctx, activeSnapshot).Scan(&after); err != nil || before != after {
				t.Fatalf("retirement changed other preferences: %v", err)
			}
			var count int
			if err := pool.QueryRow(ctx, "SELECT count(*) FROM settings WHERE key='telegram.enabled'").Scan(&count); err != nil || count != 0 {
				t.Fatalf("retired key remains active: %d %v", count, err)
			}
			expected := 0
			if value != "absent" {
				expected = 1
			}
			if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+archive+".retired_settings").Scan(&count); err != nil || count != expected {
				t.Fatalf("archive count=%d expected=%d: %v", count, expected, err)
			}
			if expected == 1 {
				if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+archive+".retired_settings WHERE key='telegram.enabled' AND value=$1 AND updated_at='2026-03-04 05:06:07+00'", value).Scan(&count); err != nil || count != 1 {
					t.Fatalf("original value/timestamp not preserved: %v", err)
				}
			}
			service := NewService(pool, slog.Default())
			if _, err := pool.Exec(ctx, `INSERT INTO settings VALUES('telegram.enabled','0',NOW())`); err == nil {
				t.Fatal("cached old producer recreated retired key")
			}
			if _, visible := service.All()["telegram.enabled"]; visible {
				t.Fatal("retired switch remains visible")
			}
			for _, rejected := range []string{"telegram.enabled", "unknown.future.preference"} {
				if err := service.Save(ctx, map[string]string{PaymentsEnabled: "0", rejected: "modified"}); !errors.Is(err, ErrUnknownSetting) {
					t.Fatalf("mixed save accepted unsupported key %q: %v", rejected, err)
				}
				if err := pool.QueryRow(ctx, activeSnapshot).Scan(&after); err != nil || before != after {
					t.Fatalf("rejected mixed save partially changed settings: %v", err)
				}
			}
			if err := service.Save(ctx, map[string]string{PaymentsEnabled: "0"}); err != nil || service.Value(PaymentsEnabled) != "0" {
				t.Fatalf("supported settings no longer save: %v", err)
			}
		})
	}
}
