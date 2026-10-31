package migrate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Execute the real migration against isolated schemas: a wrong catalogue
// identity must reject the whole batch, while retries must preserve later edits.
func TestAgreedMeasurementsAtomicImport(t *testing.T) {
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
	raw, err := os.ReadFile("../../../timeweb/migrations/20261031_agreed_plant_measurements.sql")
	if err != nil {
		t.Fatal(err)
	}
	input, err := os.ReadFile("../../../docs/agreed-plant-measurements-2026-10-31.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		SKU        string         `json:"sku"`
		ID         int64          `json:"variant_id"`
		Code       int64          `json:"product_code"`
		Name       string         `json:"expected_name"`
		OldLabel   string         `json:"expected_label"`
		Label      string         `json:"label"`
		Attributes map[string]any `json:"attributes"`
	}
	if err = json.Unmarshal(input, &rows); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"complete", "changed identity", "empty catalogue"} {
		t.Run(scenario, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			must := func(sql string, args ...any) {
				t.Helper()
				if _, err := tx.Exec(ctx, sql, args...); err != nil {
					t.Fatal(err)
				}
			}
			schema := fmt.Sprintf("measurement_test_%d", time.Now().UnixNano())
			must("CREATE SCHEMA " + schema)
			must("SET LOCAL search_path=" + schema)
			must(`CREATE TABLE products(id bigint PRIMARY KEY,product_code bigint UNIQUE,name text,catalog_section text,status text);
CREATE TABLE product_variants(id bigint PRIMARY KEY,product_id bigint,sku text UNIQUE,label text,is_active int,archived_at timestamptz,updated_at timestamptz DEFAULT now(),base_price_minor bigint DEFAULT 123456);
CREATE TABLE attribute_definitions(id bigserial PRIMARY KEY,code text UNIQUE,name text,data_type text,unit text,audience text,value_scope text,is_global bool,is_active bool,description text);
CREATE TABLE category_attributes(category_id bigint,attribute_id bigint,is_required bool,is_filterable bool,show_on_pdp bool,is_badge bool,sort_order int,show_in_summary bool,summary_position int,show_in_characteristics bool,is_excluded bool,PRIMARY KEY(category_id,attribute_id));
CREATE TABLE variant_attribute_values(variant_id bigint,attribute_id bigint,value jsonb NOT NULL,source text,updated_at timestamptz DEFAULT now(),PRIMARY KEY(variant_id,attribute_id));
CREATE TABLE admin_audit_log(id bigserial PRIMARY KEY,actor_role text,action text,entity_type text,entity_id text,before_data jsonb,after_data jsonb);`)
			must(`INSERT INTO attribute_definitions(code,data_type,value_scope,is_active)
SELECT x,'number','variant',true FROM unnest(ARRAY['height_cm','pot_diameter_cm','package_weight_grams','package_length_cm','package_width_cm','package_height_cm','package_winter_length_cm','package_winter_width_cm','package_winter_height_cm']) x`)
			must(`INSERT INTO category_attributes(category_id,attribute_id,show_on_pdp,sort_order,show_in_characteristics,is_excluded)
SELECT 1,id,true,10,true,false FROM attribute_definitions WHERE code='pot_diameter_cm'`)
			if scenario != "empty catalogue" {
				for _, r := range rows {
					must(`INSERT INTO products VALUES($1,$2,$3,'plants','published') ON CONFLICT DO NOTHING`, r.Code+100000, r.Code, r.Name)
					must(`INSERT INTO product_variants(id,product_id,sku,label,is_active) VALUES($1,$2,$3,$4,1)`, r.ID, r.Code+100000, r.SKU, r.OldLabel)
				}
				must(`INSERT INTO variant_attribute_values(variant_id,attribute_id,value,source)
SELECT v.id,d.id,'777'::jsonb,'saby' FROM product_variants v CROSS JOIN attribute_definitions d`)
			}
			if scenario == "changed identity" {
				must(`UPDATE product_variants SET label='Changed since review' WHERE sku='1'`)
			}
			must(`SAVEPOINT import_start`)
			_, err = tx.Exec(ctx, string(raw))
			if scenario == "changed identity" {
				if err == nil || !strings.Contains(err.Error(), "matched 248 of 249") {
					t.Fatalf("wrong identity was not rejected: %v", err)
				}
				must(`ROLLBACK TO SAVEPOINT import_start`)
				var changed int
				if err = tx.QueryRow(ctx, `SELECT count(*) FROM variant_attribute_values WHERE value<>'777'::jsonb`).Scan(&changed); err != nil || changed != 0 {
					t.Fatalf("partial write: %d %v", changed, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var auditCount int
			if err = tx.QueryRow(ctx, `SELECT count(*) FROM admin_audit_log`).Scan(&auditCount); err != nil {
				t.Fatal(err)
			}
			if scenario == "empty catalogue" {
				if auditCount != 0 {
					t.Fatal("empty install wrote audits")
				}
				return
			}
			if auditCount != 249 {
				t.Fatalf("audit count: %d", auditCount)
			}
			for _, r := range rows {
				var label string
				if err = tx.QueryRow(ctx, `SELECT label FROM product_variants WHERE id=$1`, r.ID).Scan(&label); err != nil || label != r.Label {
					t.Fatalf("SKU %s label %s: %v", r.SKU, label, err)
				}
				for code, want := range r.Attributes {
					var got []byte
					err = tx.QueryRow(ctx, `SELECT a.value FROM variant_attribute_values a JOIN attribute_definitions d ON d.id=a.attribute_id WHERE a.variant_id=$1 AND d.code=$2`, r.ID, code).Scan(&got)
					if want == nil {
						if err == nil {
							t.Fatalf("non-pot SKU %s retained %s", r.SKU, code)
						}
						continue
					}
					var value any
					if err != nil {
						t.Fatal(err)
					}
					if err = json.Unmarshal(got, &value); err != nil || value != want {
						t.Fatalf("%s %s = %s, want %v", r.SKU, code, got, want)
					}
				}
			}
			var backedUp int
			if err = tx.QueryRow(ctx, `SELECT count(*) FROM admin_audit_log WHERE before_data->'attributes'->'height_cm'->>'source'='saby' AND before_data->'attributes'->'height_cm'->'value'='777'::jsonb`).Scan(&backedUp); err != nil || backedUp != 249 {
				t.Fatalf("backup lost: %d %v", backedUp, err)
			}
			must(`UPDATE variant_attribute_values SET value='666'::jsonb WHERE variant_id=$1 AND attribute_id=(SELECT id FROM attribute_definitions WHERE code='height_cm')`, rows[0].ID)
			must(string(raw))
			var height string
			if err = tx.QueryRow(ctx, `SELECT value::text FROM variant_attribute_values WHERE variant_id=$1 AND attribute_id=(SELECT id FROM attribute_definitions WHERE code='height_cm')`, rows[0].ID).Scan(&height); err != nil || height != "666" {
				t.Fatalf("retry overwrote later edit: %s %v", height, err)
			}
		})
	}
}
