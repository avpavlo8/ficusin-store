package migrate

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTypedDimensionsRetirementPreservesCanonicalPIMOnDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	raw, e := os.ReadFile("../../../timeweb/migrations/20261113_zzzzzzzzzzzzz_retire_typed_variant_dimensions.sql")
	if e != nil {
		t.Fatal(e)
	}
	var numericHelper string
	if e = pool.QueryRow(ctx, `SELECT pg_get_functiondef('public.variant_numeric_attribute(bigint,text)'::regprocedure)`).Scan(&numericHelper); e != nil {
		t.Fatal(e)
	}
	for _, scenario := range []string{"canonical", "missing definition", "wrong type", "wrong scope", "source constraint", "empty"} {
		t.Run(scenario, func(t *testing.T) {
			tx, e := pool.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer tx.Rollback(ctx)
			must := func(e error) {
				t.Helper()
				if e != nil {
					t.Fatal(e)
				}
			}
			exec := func(sql string, args ...any) { t.Helper(); _, e := tx.Exec(ctx, sql, args...); must(e) }
			schema := fmt.Sprintf("retire_dimensions_%d", time.Now().UnixNano())
			archive := schema + "_archive"
			exec("CREATE SCHEMA " + schema + ";SET LOCAL search_path=" + schema)
			exec(`CREATE TABLE products(id BIGINT PRIMARY KEY,name TEXT,override_fields TEXT[],created_at TIMESTAMPTZ DEFAULT NOW(),updated_at TIMESTAMPTZ DEFAULT NOW());
CREATE TABLE product_variants(id BIGINT PRIMARY KEY,product_id BIGINT REFERENCES products(id),label TEXT,base_price_minor BIGINT,height_cm INT,pot_diameter_cm INT,package_length_cm INT,package_width_cm INT,package_height_cm INT,package_weight_grams INT,override_fields TEXT[],created_at TIMESTAMPTZ DEFAULT NOW(),updated_at TIMESTAMPTZ DEFAULT NOW());
CREATE TABLE attribute_definitions(id BIGINT PRIMARY KEY,code TEXT UNIQUE,data_type TEXT,value_scope TEXT);
INSERT INTO attribute_definitions VALUES(1,'height_cm','number','variant'),(2,'pot_diameter_cm','number','variant'),(3,'package_length_cm','number','variant'),(4,'package_width_cm','number','variant'),(5,'package_height_cm','number','variant'),(6,'package_weight_grams','number','variant');
CREATE TABLE variant_attribute_values(variant_id BIGINT REFERENCES product_variants(id),attribute_id BIGINT REFERENCES attribute_definitions(id) ON DELETE CASCADE,value JSONB NOT NULL,source TEXT NOT NULL,updated_at TIMESTAMPTZ DEFAULT NOW(),PRIMARY KEY(variant_id,attribute_id),CONSTRAINT source_check CHECK(source IN('local','saby','import')));
CREATE TABLE inventory(variant_id BIGINT REFERENCES product_variants(id),available_qty INT,reserved_qty INT,synced_at TIMESTAMPTZ);
CREATE TABLE historical_audit(data JSONB);INSERT INTO historical_audit VALUES('{"variant":{"height_cm":35,"override_fields":["price"]}}');`)
			exec(strings.Replace(numericHelper, "public.variant_numeric_attribute", schema+".variant_numeric_attribute", 1))
			if scenario != "empty" {
				exec(`INSERT INTO products VALUES(1,'Current product',ARRAY['name',NULL,'name'],TIMESTAMPTZ '2020-01-02 03:04:05+00',TIMESTAMPTZ '2021-02-03 04:05:06+00');
INSERT INTO product_variants(id,product_id,label,base_price_minor,height_cm,pot_diameter_cm,package_length_cm,package_width_cm,package_height_cm,package_weight_grams,override_fields) VALUES
 (1,1,'Missing PIM',149099,40,12,20,15,45,1250,ARRAY['dimensions','price']),
 (2,1,'Current PIM',299099,50,17,30,25,65,2250,ARRAY[]::TEXT[]),
 (3,1,'Zero and negative',399099,0,0,-2,0,NULL,0,ARRAY['price',NULL]),
 (4,1,'Null columns',499099,NULL,NULL,NULL,NULL,NULL,NULL,ARRAY[]::TEXT[]);
INSERT INTO variant_attribute_values(variant_id,attribute_id,value,source,updated_at) VALUES
 (2,1,'"21.5"','local',TIMESTAMPTZ '2022-02-03 04:05:06+00'),(2,2,'null','local',TIMESTAMPTZ '2022-02-03 04:05:06+00'),
 (2,3,'99','local',TIMESTAMPTZ '2022-02-03 04:05:06+00'),(2,4,'0','local',TIMESTAMPTZ '2022-02-03 04:05:06+00'),
 (2,5,'-5','saby',TIMESTAMPTZ '2022-02-03 04:05:06+00'),(2,6,'null','local',TIMESTAMPTZ '2022-02-03 04:05:06+00');
INSERT INTO inventory VALUES(1,7,2,NOW()),(2,4,0,NOW());`)
			}
			switch scenario {
			case "missing definition":
				exec(`DELETE FROM attribute_definitions WHERE id=1`)
			case "wrong type":
				exec(`UPDATE attribute_definitions SET data_type='text' WHERE id=1`)
			case "wrong scope":
				exec(`UPDATE attribute_definitions SET value_scope='product' WHERE id=1`)
			case "source constraint":
				exec(`ALTER TABLE variant_attribute_values DROP CONSTRAINT source_check;ALTER TABLE variant_attribute_values ADD CONSTRAINT source_check CHECK(source IN('local','saby'))`)
			}
			exec(`CREATE TEMP TABLE dimensions_before AS SELECT id,to_jsonb(v) original,to_jsonb(v)-ARRAY['height_cm','pot_diameter_cm','package_length_cm','package_width_cm','package_height_cm','package_weight_grams','override_fields'] current FROM product_variants v;
CREATE TEMP TABLE products_before AS SELECT id,to_jsonb(p) original,to_jsonb(p)-'override_fields' current FROM products p;
CREATE TEMP TABLE pim_before AS SELECT to_jsonb(v) original FROM variant_attribute_values v;
CREATE TEMP TABLE inventory_before AS SELECT to_jsonb(i) original FROM inventory i;
CREATE TEMP TABLE audit_before AS SELECT data FROM historical_audit;
SAVEPOINT retirement`)
			_, e = tx.Exec(ctx, strings.ReplaceAll(string(raw), "legacy_archive", archive))
			if scenario != "canonical" && scenario != "empty" {
				if e == nil {
					t.Fatal("unsafe definition/source configuration was retired")
				}
				exec("ROLLBACK TO SAVEPOINT retirement")
				var n int
				must(tx.QueryRow(ctx, `SELECT count(*) FROM product_variants v JOIN dimensions_before b USING(id) WHERE to_jsonb(v) IS DISTINCT FROM b.original`).Scan(&n))
				if n != 0 {
					t.Fatal("failed migration changed source rows")
				}
				var absent bool
				must(tx.QueryRow(ctx, `SELECT to_regclass($1) IS NULL`, archive+".variant_retired_dimensions").Scan(&absent))
				if !absent {
					t.Fatal("failed migration retained a partial archive")
				}
				return
			}
			must(e)
			var n int
			must(tx.QueryRow(ctx, `SELECT count(*) FROM product_variants v JOIN dimensions_before b USING(id) WHERE to_jsonb(v) IS DISTINCT FROM b.current`).Scan(&n))
			if n != 0 {
				t.Fatal("current variant identity, price or timestamps changed")
			}
			must(tx.QueryRow(ctx, `SELECT count(*) FROM products p JOIN products_before b USING(id) WHERE to_jsonb(p) IS DISTINCT FROM b.current`).Scan(&n))
			if n != 0 {
				t.Fatal("current product changed")
			}
			must(tx.QueryRow(ctx, "SELECT count(*) FROM "+archive+`.variant_retired_dimensions a JOIN dimensions_before b ON b.id=a.variant_id WHERE (to_jsonb(a)-ARRAY['variant_id','archived_at']) IS DISTINCT FROM (b.original-ARRAY['id','label','base_price_minor'])`).Scan(&n))
			if n != 0 {
				t.Fatal("typed dimensions or override arrays changed in archive")
			}
			must(tx.QueryRow(ctx, "SELECT count(*) FROM "+archive+`.product_retired_overrides a JOIN products_before b ON b.id=a.product_id WHERE (to_jsonb(a)-ARRAY['product_id','archived_at']) IS DISTINCT FROM (b.original-ARRAY['id','name'])`).Scan(&n))
			if n != 0 {
				t.Fatal("product override archive changed")
			}
			must(tx.QueryRow(ctx, `SELECT count(*) FROM pim_before b LEFT JOIN variant_attribute_values v ON to_jsonb(v)=b.original WHERE v.variant_id IS NULL`).Scan(&n))
			if n != 0 {
				t.Fatal("current PIM values/source/timestamps were overwritten")
			}
			must(tx.QueryRow(ctx, `SELECT count(*) FROM inventory_before b FULL JOIN inventory i ON to_jsonb(i)=b.original WHERE b.original IS NULL OR i.variant_id IS NULL`).Scan(&n))
			if n != 0 {
				t.Fatal("inventory changed")
			}
			must(tx.QueryRow(ctx, `SELECT count(*) FROM audit_before b FULL JOIN historical_audit a ON a.data=b.data WHERE a.data IS NULL OR b.data IS NULL`).Scan(&n))
			if n != 0 {
				t.Fatal("historical JSON changed")
			}
			if scenario == "canonical" {
				must(tx.QueryRow(ctx, `SELECT count(*) FROM variant_attribute_values WHERE source='import'`).Scan(&n))
				if n != 11 {
					t.Fatalf("missing-only backfill changed: %d", n)
				}
				var height, weight, oldZero, negative float64
				var explicitNull, missing *float64
				must(tx.QueryRow(ctx, `SELECT variant_numeric_attribute(1,'height_cm')::float8,variant_numeric_attribute(1,'package_weight_grams')::float8,variant_numeric_attribute(3,'package_weight_grams')::float8,variant_numeric_attribute(3,'package_length_cm')::float8,variant_numeric_attribute(2,'pot_diameter_cm')::float8,variant_numeric_attribute(4,'height_cm')::float8`).Scan(&height, &weight, &oldZero, &negative, &explicitNull, &missing))
				if height != 40 || weight != 1250 || oldZero != 0 || negative != -2 || explicitNull != nil || missing != nil {
					t.Fatal("production PIM helper lost backfilled/explicit null semantics")
				}
				must(tx.QueryRow(ctx, `SELECT variant_numeric_attribute(2,'height_cm')::float8`).Scan(&height))
				if height != 21.5 {
					t.Fatal("existing numeric-string PIM value was replaced")
				}
			}
		})
	}
}
