package admin

import (
	"context"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/saby"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSabyLegacyFlagsCannotOverwriteSiteContentOnLiveDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	base, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer base.Close()
	// Sync imports a complete catalogue snapshot. Isolate its tables so it
	// cannot mark another test's Saby directory missing or refresh its stock.
	schema := fmt.Sprintf("saby_content_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, e = base.Exec(ctx, "CREATE SCHEMA "+quoted); e != nil {
		t.Fatal(e)
	}
	defer base.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE")
	config, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, e := pgxpool.NewWithConfig(ctx, config)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	exec := func(sql string, args ...any) { t.Helper(); _, e := pool.Exec(ctx, sql, args...); must(e) }
	for _, table := range []string{"customers", "products", "product_variants", "product_external_ids", "product_media", "warehouses", "inventory", "saby_nomenclature", "saby_catalog_folders", "admin_audit_log", "sync_runs", "procurement_sales_daily", "attribute_definitions", "categories", "category_attributes", "attribute_options", "product_attribute_values", "variant_attribute_values"} {
		id := pgx.Identifier{table}.Sanitize()
		exec("CREATE TABLE " + id + " (LIKE public." + id + " INCLUDING DEFAULTS INCLUDING GENERATED INCLUDING IDENTITY INCLUDING CONSTRAINTS INCLUDING INDEXES)")
	}
	exec(`CREATE VIEW canonical_product_directory AS SELECT v.id variant_id,v.product_id,v.saby_id FROM product_variants v`)
	var actorID, product, variant int64
	must(pool.QueryRow(ctx, `INSERT INTO customers(phone,password_hash,full_name,consent_at) VALUES('+70000000000','','Saby ownership owner',NOW()) RETURNING id`).Scan(&actorID))
	must(pool.QueryRow(ctx, `INSERT INTO products(name,slug,description,status,saby_fields) VALUES('Название сайта','site-content-fixture','Описание сайта','draft',ARRAY['name','description','photo','price']) RETURNING id`).Scan(&product))
	must(pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,label,base_price_minor,is_active) VALUES($1,'D12',149099,1) RETURNING id`, product).Scan(&variant))
	exec(`INSERT INTO product_media(product_id,variant_id,object_key,alt_text,sort_order,is_primary) VALUES($1,NULL,'https://site.example.test/cover.jpg','Обложка сайта',3,1),($1,$2,'https://site.example.test/detail.jpg','Деталь сайта',7,0)`, product, variant)
	exec(`INSERT INTO saby_nomenclature(saby_id,code,name) VALUES('771','X771','Исходный справочник')`)
	repository := NewPostgresRepository(pool)
	actor := Actor{CustomerID: actorID, Role: RoleOwner}
	must(repository.LinkVariantToSaby(ctx, actor, variant, "771"))
	var galleryBefore []byte
	must(pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(m) ORDER BY id) FROM product_media m WHERE product_id=$1`, product).Scan(&galleryBefore))
	var flags []string
	must(pool.QueryRow(ctx, `SELECT saby_fields FROM products WHERE id=$1`, product).Scan(&flags))
	if !slices.Contains(flags, "photo") || !slices.Contains(flags, "stock") {
		t.Fatal("fixture did not retain legacy flags through attachment")
	}
	images := []string{"https://saby.example.test/source.jpg"}
	result, e := saby.NewService(pool, nil).Sync(ctx, []saby.CatalogItem{
		{ID: "771", NomNumber: "X771", Name: "Название из СБИС", Description: "<p>Описание из СБИС</p>", Cost: 9900.0, Balance: 8.0, Images: images},
		{ID: "772", NomNumber: "X772", Name: "Новый импорт из СБИС", Description: "<p>Сырьё для черновика</p>", Cost: 3490.0, Balance: 4.0, Images: images},
	})
	must(e)
	if !result.OK || result.ItemsRead != 2 {
		t.Fatalf("sync failed: %+v", result)
	}
	var name, description string
	var price int64
	var galleryAfter []byte
	must(pool.QueryRow(ctx, `SELECT p.name,p.description,v.base_price_minor FROM products p JOIN product_variants v ON v.product_id=p.id WHERE v.id=$1`, variant).Scan(&name, &description, &price))
	must(pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(m) ORDER BY id) FROM product_media m WHERE product_id=$1`, product).Scan(&galleryAfter))
	if name != "Название сайта" || description != "Описание сайта" || price != 149099 || string(galleryBefore) != string(galleryAfter) {
		t.Fatalf("sync overwrote site content: %s/%s/%d; gallery %s -> %s", name, description, price, galleryBefore, galleryAfter)
	}
	var qty int
	var fresh bool
	must(pool.QueryRow(ctx, `SELECT available_qty,synced_at>=NOW()-INTERVAL '1 minute' FROM inventory WHERE variant_id=$1`, variant).Scan(&qty, &fresh))
	if qty != 8 || !fresh {
		t.Fatalf("stock stopped refreshing: %d/%t", qty, fresh)
	}
	var sourceName, sourceDescription string
	var sourcePrice int64
	var sourceImages []string
	must(pool.QueryRow(ctx, `SELECT name,description,price_minor,images FROM saby_nomenclature WHERE saby_id='771'`).Scan(&sourceName, &sourceDescription, &sourcePrice, &sourceImages))
	if sourceName != "Название из СБИС" || sourceDescription != "Описание из СБИС" || sourcePrice != 990000 || !slices.Equal(sourceImages, images) {
		t.Fatalf("raw import source lost: %s/%s/%d/%v", sourceName, sourceDescription, sourcePrice, sourceImages)
	}
	imported, e := repository.ImportProducts(ctx, actor, ImportRequest{Codes: []string{"X772"}})
	must(e)
	if imported.Created != 1 || len(imported.Entries) != 1 || imported.Entries[0].ProductID == nil {
		t.Fatalf("initial import failed: %+v", imported)
	}
	var status string
	must(pool.QueryRow(ctx, `SELECT p.name,p.description,p.status,v.base_price_minor,ARRAY(SELECT object_key FROM product_media WHERE product_id=p.id ORDER BY sort_order,id) FROM products p JOIN product_variants v ON v.product_id=p.id WHERE p.id=$1`, *imported.Entries[0].ProductID).Scan(&name, &description, &status, &price, &sourceImages))
	if name != "Новый импорт из СБИС" || description != "Сырьё для черновика" || status != "draft" || price != 349000 || !slices.Equal(sourceImages, images) {
		t.Fatalf("initial import no longer copies mirror into draft: %s/%s/%s/%d/%v", name, description, status, price, sourceImages)
	}
}
