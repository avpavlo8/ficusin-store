package admin

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
	"time"
)

func TestEditorPlanValidation(t *testing.T) {
	p := EditorPlan{Variants: []EditorVariantPlan{{VariantID: 1}}}
	if err := ValidateEditorPlan(p, false); err != nil {
		t.Fatal(err)
	}
	p.Variants[0].Marketplace.Ozon = EditorChannel{Enabled: true, Mode: "link"}
	if ValidateEditorPlan(p, true) == nil {
		t.Fatal("empty existing article accepted")
	}
	p.Variants[0].Marketplace.Ozon.ExistingID = "123"
	if err := ValidateEditorPlan(p, true); err != nil {
		t.Fatal(err)
	}
	p.Variants[0].Marketplace.Ozon.Mode = "create"
	if ValidateEditorPlan(p, true) == nil {
		t.Fatal("missing required values accepted")
	}
}
func TestEditorPlanOnLiveDatabase(t *testing.T) {
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
	tag := fmt.Sprint(time.Now().UnixNano())
	var customer int64
	if e = pool.QueryRow(ctx, `INSERT INTO customers(email,phone,full_name,consent_at) VALUES($1,$2,'Channel plan test',NOW()) RETURNING id`, tag+"@example.invalid", "+7"+tag[len(tag)-10:]).Scan(&customer); e != nil {
		t.Fatal(e)
	}
	actor := Actor{CustomerID: customer, Role: RoleOwner}
	repo := NewPostgresRepository(pool)
	p, e := repo.CreateProduct(ctx, actor, ProductCreate{Name: "Plan test " + tag, PriceMinor: 10000, CatalogSection: "plants"})
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		pool.Exec(ctx, `DELETE FROM catalog_editor_publications WHERE variant_id IN(SELECT id FROM product_variants WHERE product_id=$1)`, p.ID)
		pool.Exec(ctx, `DELETE FROM admin_audit_log WHERE actor_customer_id=$1`, customer)
		pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, p.ID)
		pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, customer)
	}()
	variants, e := repo.ListProductVariants(ctx, p.ID)
	if e != nil {
		t.Fatal(e)
	}
	id := variants[0].ID
	plan := EditorPlan{Variants: []EditorVariantPlan{{VariantID: id, Saby: EditorSaby{Mode: "create", Category: EditorCategory{ID: "fixture-category"}, Folder: EditorCategory{ID: "fixture-folder"}}, Marketplace: EditorMarketplace{Model: "Тестовая модель", Units: 1}}}}
	v, e := repo.SaveEditorPlan(ctx, actor, p.ID, plan, true)
	if e != nil {
		t.Fatal(e)
	}
	code := v.Barcodes[id]
	if len(code) != 13 || code[:3] != "200" {
		t.Fatalf("invalid EAN: %s", code)
	}
	sum := 0
	for i, ch := range code {
		n := int(ch - '0')
		if i%2 == 1 {
			n *= 3
		}
		sum += n
	}
	if sum%10 != 0 {
		t.Fatalf("bad EAN checksum: %s", code)
	}
	second, e := repo.SaveEditorPlan(ctx, actor, p.ID, plan, true)
	if e != nil {
		t.Fatal(e)
	}
	if second.Barcodes[id] != code || len(second.Publications) != 1 {
		t.Fatal("repeat save duplicated barcode or request")
	}
	read, e := repo.ReadEditorPlan(ctx, p.ID)
	if e != nil || read.Config.Variants[0].Marketplace.Model != "Тестовая модель" {
		t.Fatalf("plan not persisted: %v", e)
	}
	bad := plan
	bad.Variants = append(append([]EditorVariantPlan{}, plan.Variants...), EditorVariantPlan{VariantID: 999999999, Saby: plan.Variants[0].Saby})
	if _, e = repo.SaveEditorPlan(ctx, actor, p.ID, bad, true); e == nil {
		t.Fatal("foreign variant accepted")
	}
	read, e = repo.ReadEditorPlan(ctx, p.ID)
	if e != nil || len(read.Config.Variants) != 1 {
		t.Fatal("failed save changed the plan")
	}
}

func TestEditorSabyVariantName(t *testing.T) {
	for _, name := range []string{"Бонсай", "Бонсай D12", "Бонсай D13", "Бонсай д 12 см"} {
		if got := editorSabyName(name, map[string]any{"pot_diameter_cm": 13}); got != "Бонсай D13" {
			t.Fatalf("%s became %s", name, got)
		}
	}
	if got := editorSabyName("Грунт 5 л", nil); got != "Грунт 5 л" {
		t.Fatal(got)
	}
}

func TestEditorModelAndUnitsAreOzonSpecific(t *testing.T) {
	m := EditorMarketplace{Article: "test", Brand: "brand", TNVED: "060290", WB: EditorChannel{Enabled: true, Mode: "create", Category: EditorCategory{ID: "10"}}}
	p := EditorPlan{Variants: []EditorVariantPlan{{VariantID: 1, Marketplace: m}}}
	if err := ValidateEditorPlan(p, true); err != nil {
		t.Fatal(err)
	}
	p.Variants[0].Marketplace.Ozon = EditorChannel{Enabled: true, Mode: "create", Category: EditorCategory{ID: "20", TypeID: 30}}
	if ValidateEditorPlan(p, true) == nil {
		t.Fatal("Ozon accepted missing model and units")
	}
}
