package procurement

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPricingSettingsRoundTripAndVersionGuardOnDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	bootstrap, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer bootstrap.Close()
	schema := fmt.Sprintf("pricing_roundtrip_%d", time.Now().UnixNano())
	if _, err = bootstrap.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer bootstrap.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE") // Disposable test fixture only.
	for _, table := range []string{"procurement_pricing_settings", "admin_audit_log"} {
		if _, err = bootstrap.Exec(ctx, "CREATE TABLE "+schema+"."+table+"(LIKE public."+table+" INCLUDING DEFAULTS INCLUDING CONSTRAINTS)"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = bootstrap.Exec(ctx, "INSERT INTO "+schema+".procurement_pricing_settings SELECT * FROM public.procurement_pricing_settings"); err != nil {
		t.Fatal(err)
	}
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
	store := NewPostgresStore(pool)
	service := NewServiceWithExecutor(store, nil)
	initial, err := store.loadSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("unified=%v", enabled), func(t *testing.T) {
			input, err := store.loadSettings(ctx)
			if err != nil {
				t.Fatal(err)
			}
			unified := unifiedSettings(t)
			input.TaxRate, input.Economics = unified.TaxRate, unified.Economics
			input.Economics.Unified.Enabled = enabled
			input.RetailMarkupMultiplier = 2.25
			input.RoundPrices = false
			previousVersion := input.Version
			saved, err := service.UpdateSettings(ctx, Actor{CustomerID: 77, Role: "owner"}, input)
			if err != nil {
				t.Fatal(err)
			}
			if saved.Version != previousVersion+1 || saved.RetailMarkupMultiplier != 2.25 || saved.RoundPrices || saved.Economics.Unified.Enabled != enabled || saved.TrolleyVolumeCM3 != initial.TrolleyVolumeCM3 || saved.TrolleyFillRatio != initial.TrolleyFillRatio {
				t.Fatalf("settings roundtrip lost current knobs: %+v", saved)
			}
			if _, err = service.UpdateSettings(ctx, Actor{CustomerID: 77, Role: "owner"}, input); err == nil {
				t.Fatal("stale version overwrote current settings")
			}
			var audits int
			if err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM admin_audit_log WHERE action='procurement.settings.update' AND (after_data->>'version')::int=$1`, saved.Version).Scan(&audits); err != nil || audits != 1 {
				t.Fatalf("successful save audit missing/duplicated: %d %v", audits, err)
			}
			preview, err := store.PreviewUnitPrice(ctx, UnitPriceInput{Mode: "cost", CostRUB: 1000, Package: &PricingPackage{LengthCM: 15, WidthCM: 15, HeightCM: 40}})
			if err != nil || preview.RetailRUB <= 0 || preview.Quotes["wb"].PriceRUB == nil {
				t.Fatalf("saved formula no longer calculates: %+v %v", preview, err)
			}
		})
	}
}
