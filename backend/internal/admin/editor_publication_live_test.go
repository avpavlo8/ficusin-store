package admin

import (
	"context"
	"encoding/json"
	"fmt"
	ce "github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/avpavlo8/ficusin-store/backend/internal/migrate"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestEditorPublicationLocalDatabase(t *testing.T) {
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
	var db string
	pool.QueryRow(ctx, "SELECT current_database()").Scan(&db)
	if db != "ficusin_local_preview" && db != "ficusin_local_publisher_test" {
		t.Fatal("isolated local database required")
	}
	if db == "ficusin_local_publisher_test" {
		if err := migrate.Apply(ctx, pool, "../../../timeweb/migrations"); err != nil {
			t.Fatal(err)
		}
	}
	tag := fmt.Sprint(time.Now().UnixNano())
	var customer int64
	if e = pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at) VALUES($1,$2,'','Publisher local test',NOW()) RETURNING id`, tag+"@example.invalid", "+7"+tag[len(tag)-10:]).Scan(&customer); e != nil {
		t.Fatal(e)
	}
	actor := Actor{CustomerID: customer, Role: RoleOwner}
	repo := NewPostgresRepository(pool).WithEditorPublicationMode("mock")
	p, e := repo.CreateProduct(ctx, actor, ProductCreate{Name: "Локальная проверка " + tag, Description: "Описание", PriceMinor: 10000, CatalogSection: "plants", VariantAttributes: map[string]any{"pot_diameter_cm": float64(13), "height_cm": float64(28), "package_length_cm": float64(30), "package_width_cm": float64(20), "package_height_cm": float64(45), "package_weight_grams": float64(1250)}})
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		pool.Exec(ctx, `DELETE FROM catalog_editor_price_publications WHERE variant_id IN(SELECT id FROM product_variants WHERE product_id=$1)`, p.ID)
		pool.Exec(ctx, `DELETE FROM catalog_editor_remote_links WHERE variant_id IN(SELECT id FROM product_variants WHERE product_id=$1)`, p.ID)
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
	// These test-only characteristics are never written into a user's draft.
	for code, n := range map[string]int{"package_length_cm": 30, "package_width_cm": 20, "package_height_cm": 45, "package_winter_length_cm": 35, "package_winter_width_cm": 25, "package_winter_height_cm": 50, "package_weight_grams": 1250} {
		if _, e = pool.Exec(ctx, `INSERT INTO variant_attribute_values(variant_id,attribute_id,value) SELECT $1,id,to_jsonb($3::int) FROM attribute_definitions WHERE code=$2 ON CONFLICT(variant_id,attribute_id) DO UPDATE SET value=EXCLUDED.value`, id, code, n); e != nil {
			t.Fatal(e)
		}
	}
	// Old editors copied the shared cover into SKU rows. The publication
	// snapshot must include each public photo once and retain own SKU photos.
	_, e = pool.Exec(ctx, `INSERT INTO product_media(product_id,variant_id,object_key,sort_order,is_primary) VALUES
	 ($1,$2,'https://images.example.test/own.jpg',0,1),
	 ($1,NULL,'https://images.example.test/cover.jpg',0,1),
	 ($1,$2,'https://images.example.test/cover.jpg',1,0),
	 ($1,NULL,'https://images.example.test/own.jpg',1,0)`, p.ID, id)
	if e != nil {
		t.Fatal(e)
	}
	mock := integration.NewMockCatalog()
	terminal := func(status string) bool { return status == "created" || status == "synced" || status == "linked" }
	drive := func(limit int) {
		t.Helper()
		for i := 0; i < limit; i++ {
			if err := ce.PublicationStep(ctx, pool, mock, "mock"); err != nil {
				t.Fatal(err)
			}
			var active int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM catalog_editor_publications WHERE variant_id IN(SELECT id FROM product_variants WHERE product_id=$1) AND channel IN('wb','ozon') AND status IN('prepared','queued','processing')`, p.ID).Scan(&active); err != nil {
				t.Fatal(err)
			}
			if active == 0 {
				return
			}
		}
		t.Fatal("card publications did not reach a terminal receipt")
	}
	for _, ch := range []string{"ozon", "wb"} {
		category, typ := int64(900001), int64(900002)
		if ch == "wb" {
			category = 900003
			typ = 0
		}
		raw, e := mock.Executor.ReadEditorReference(ctx, ch, "attributes", category, typ, 0, "")
		if e != nil {
			t.Fatal(e)
		}
		if e = repo.CacheEditorReference(ctx, ch, "attributes", fmt.Sprintf("%d:%d:0:", category, typ), raw); e != nil {
			t.Fatal(e)
		}
	}
	for _, attr := range []int64{85, 22232, 90002} {
		raw, e := mock.Executor.ReadEditorReference(ctx, "ozon", "values", 900001, 900002, attr, "")
		if e != nil {
			t.Fatal(e)
		}
		if e = repo.CacheEditorReference(ctx, "ozon", "values", fmt.Sprintf("900001:900002:%d:", attr), raw); e != nil {
			t.Fatal(e)
		}
	}
	m := EditorMarketplace{Name: "Локальная Нолина", Description: "Описание площадки", Article: "local-" + tag, Brand: "Фикусин", BrandID: 900101, TNVED: "0602909900", TNVEDID: 900102, Model: "Нолина", Units: 1, Ozon: EditorChannel{Mode: "create", Enabled: true, Category: EditorCategory{ID: "900001", TypeID: 900002}, Attributes: map[string]any{"90002": []ce.Value{{Value: "Россия", DictionaryID: 900103}}}}, WB: EditorChannel{Mode: "create", Enabled: true, Category: EditorCategory{ID: "900003"}, Attributes: map[string]any{"15000001": "Россия", "15000002": "13"}}}
	plan := EditorPlan{Variants: []EditorVariantPlan{{VariantID: id, Saby: EditorSaby{Mode: "create", Category: EditorCategory{ID: "local"}, Folder: EditorCategory{ID: "local"}}, Marketplace: m, OzonPrice: "5000", WBPrice: "4000"}}}
	if _, e = repo.SaveEditorPlan(ctx, actor, p.ID, plan, true); e != nil {
		t.Fatal(e)
	}
	var frozen ce.Request
	if e = pool.QueryRow(ctx, `SELECT payload FROM catalog_editor_publications WHERE variant_id=$1 AND channel='ozon'`, id).Scan(&frozen); e != nil {
		t.Fatal(e)
	}
	if len(frozen.Schema) == 0 || frozen.Packaging.Weight != 1250 || len(frozen.Images) != 2 || frozen.Images[0] != "https://images.example.test/cover.jpg" || frozen.Images[1] != "https://images.example.test/own.jpg" {
		t.Fatalf("snapshot incomplete: %+v", frozen)
	}
	drive(20)
	view, e := repo.ReadEditorPlan(ctx, p.ID)
	if e != nil {
		t.Fatal(e)
	}
	count := 0
	for _, v := range view.Publications {
		if v.Channel == "saby" {
			continue
		}
		if v.Status != "created" || v.ExternalIDs.ProductID == "" || v.ExecutionMode != "mock" {
			data, _ := json.Marshal(v)
			t.Fatalf("did not finish: %s", data)
		}
		count++
	}
	if count != 2 {
		t.Fatal("missing channel statuses")
	}
	before := map[string]string{}
	for _, publication := range view.Publications {
		before[publication.Channel] = publication.ExternalIDs.ProductID
	}
	var leaked int
	if e = pool.QueryRow(ctx, `SELECT COUNT(*) FROM product_external_ids WHERE variant_id=$1 AND provider IN ('ozon','wildberries')`, id).Scan(&leaked); e != nil || leaked != 0 {
		t.Fatal("mock IDs leaked into working integrations")
	}
	// Repeated Save retains the receipt and must not recreate cards.
	if _, e = repo.SaveEditorPlan(ctx, actor, p.ID, plan, true); e != nil {
		t.Fatal(e)
	}
	for _, channel := range []string{"wb", "ozon"} {
		var status string
		pool.QueryRow(ctx, `SELECT status FROM catalog_editor_publications WHERE variant_id=$1 AND channel=$2`, id, channel).Scan(&status)
		if status != "created" {
			t.Fatal("repeat save reset publication")
		}
	}
	var drafts, autoApproved int
	if err := pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE status<>'draft' OR approved_at IS NOT NULL OR execution_mode<>'disabled') FROM catalog_editor_price_publications WHERE variant_id=$1`, id).Scan(&drafts, &autoApproved); err != nil {
		t.Fatal(err)
	}
	if drafts != 2 || autoApproved != 0 {
		t.Fatalf("content save must leave exactly two unapproved price drafts: %d/%d", drafts, autoApproved)
	}
	plan.Variants[0].Marketplace.Description = "Изменённое описание"
	plan.Variants[0].Marketplace.WB.Enabled = false
	plan.Variants[0].Marketplace.Ozon.Enabled = false
	plan.Variants[0].Marketplace.WB.Mode = "link"
	plan.Variants[0].Marketplace.Ozon.Mode = "link"
	plan.Variants[0].Marketplace.WB.ExistingID = ""
	plan.Variants[0].Marketplace.Ozon.ExistingID = ""
	plan.Variants[0].Marketplace.Brand = ""
	plan.Variants[0].Marketplace.BrandID = 0
	plan.Variants[0].Marketplace.TNVED = ""
	plan.Variants[0].Marketplace.TNVEDID = 0
	plan.Variants[0].Marketplace.Model = ""
	plan.Variants[0].Marketplace.Units = 0

	if _, e = repo.SaveEditorPlan(ctx, actor, p.ID, plan, true); e != nil {
		t.Fatal(e)
	}
	drive(20)
	view, e = repo.ReadEditorPlan(ctx, p.ID)
	if e != nil {
		t.Fatal(e)
	}
	for _, publication := range view.Publications {
		if publication.Channel == "saby" {
			continue
		}
		if !terminal(publication.Status) || publication.ExternalIDs.ProductID != before[publication.Channel] {
			t.Fatalf("edit duplicated or lost card: %+v", publication)
		}
	}

	for _, channel := range []string{"wb", "ozon"} {
		card, err := mock.Executor.ReadEditorCardByArticle(ctx, channel, m.Article)
		if err != nil || card == nil {
			t.Fatalf("updated card missing: %v", err)
		}
		want := float64(4000)
		if channel == "ozon" {
			want = 5000
		}
		actual := card.Card.WBPrice
		if channel == "ozon" {
			actual = card.Card.OzonPrice
		}
		got, err := strconv.ParseFloat(actual, 64)
		if err != nil || got != want {
			t.Fatalf("content update changed unapproved %s price: %s want %v", channel, actual, want)
		}
	}

	// Creation destinations do not control established links. New edits wait
	// behind an in-flight update, and only the latest snapshot is promoted.
	plan.Variants[0].Marketplace.Description = "Отправка A"
	if _, e = repo.SaveEditorPlan(ctx, actor, p.ID, plan, true); e != nil {
		t.Fatal(e)
	}
	if e = ce.PublicationStep(ctx, pool, mock, "mock"); e != nil {
		t.Fatal(e)
	}
	plan.Variants[0].Marketplace.Description = "Отправка B"
	if _, e = repo.SaveEditorPlan(ctx, actor, p.ID, plan, true); e != nil {
		t.Fatal(e)
	}
	var pending int
	if e = pool.QueryRow(ctx, `SELECT COUNT(*) FROM catalog_editor_publications WHERE variant_id=$1 AND pending_payload IS NOT NULL`, id).Scan(&pending); e != nil || pending < 1 {
		t.Fatalf("edit was not queued: %d %v", pending, e)
	}
	drive(45)
	for _, channel := range []string{"wb", "ozon"} {
		var desc, status string
		if e = pool.QueryRow(ctx, `SELECT payload->'marketplace'->>'description',status FROM catalog_editor_publications WHERE variant_id=$1 AND channel=$2`, id, channel).Scan(&desc, &status); e != nil || desc != "Отправка B" || !terminal(status) {
			var message string
			pool.QueryRow(ctx, `SELECT message FROM catalog_editor_publications WHERE variant_id=$1 AND channel=$2`, id, channel).Scan(&message)
			t.Fatalf("latest edit lost for %s: %s %s %v %s", channel, desc, status, e, message)
		}
	}
	// Imported canonical links are just as authoritative as this editor's links.
	if _, e = pool.Exec(ctx, `DELETE FROM catalog_editor_remote_links WHERE variant_id=$1`, id); e != nil {
		t.Fatal(e)
	}
	for _, link := range []struct{ provider, kind, value string }{{"wildberries", "vendor_code", m.Article}, {"ozon", "offer_id", m.Article}} {
		if _, e = pool.Exec(ctx, `INSERT INTO product_external_ids(product_id,variant_id,provider,id_type,external_id,source) VALUES($1,$2,$3,$4,$5,'manual')`, p.ID, id, link.provider, link.kind, link.value); e != nil {
			t.Fatal(e)
		}
	}
	plan.Variants[0].Marketplace.Description = "Правка импортированной карточки"
	if _, e = repo.SaveEditorPlan(ctx, actor, p.ID, plan, true); e != nil {
		t.Fatal(e)
	}
	drive(22)
	view, e = repo.ReadEditorPlan(ctx, p.ID)
	if e != nil {
		t.Fatal(e)
	}
	for _, publication := range view.Publications {
		if publication.Channel == "saby" {
			continue
		}
		if !terminal(publication.Status) || publication.ExternalIDs.ProductID != before[publication.Channel] {
			t.Fatalf("canonical link was not updated: %+v", publication)
		}
	}
	var otherID int64
	if e = pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,label,base_price_minor,is_active) VALUES($1,'Проверка конфликта',10000,1) RETURNING id`, p.ID).Scan(&otherID); e != nil {
		t.Fatal(e)
	}
	linked := plan.Variants[0]
	linked.VariantID = otherID
	linked.Marketplace.Ozon.Enabled = false
	linked.Marketplace.Ozon.Mode = "create"
	linked.Marketplace.WB.Mode = "link"
	linked.Marketplace.WB.Enabled = false
	linked.Marketplace.WB.ExistingID = before["wb"]
	if _, e = repo.SaveEditorPlan(ctx, actor, p.ID, EditorPlan{Variants: []EditorVariantPlan{linked}}, true); e != nil {
		t.Fatal(e)
	}
	drive(4)
	var conflictStatus string
	if e = pool.QueryRow(ctx, `SELECT status FROM catalog_editor_publications WHERE variant_id=$1 AND channel='wb'`, otherID).Scan(&conflictStatus); e != nil || conflictStatus != "failed" {
		t.Fatalf("duplicate remote link accepted: %s %v", conflictStatus, e)
	}

	// Independent seller articles replace only the requested channel links.
	aWB, aOzon := "replacement-wb-"+tag, "replacement-ozon-"+tag
	replacement := m
	replacement.WB.Article = &aWB
	replacement.WB.ReplaceLink = true
	replacement.Ozon.Article = &aOzon
	replacement.Ozon.ReplaceLink = true
	plan.Variants[0].Marketplace = replacement
	if _, e = repo.SaveEditorPlan(ctx, actor, p.ID, plan, true); e != nil {
		t.Fatal(e)
	}
	drive(32)
	view, e = repo.ReadEditorPlan(ctx, p.ID)
	if e != nil {
		t.Fatal(e)
	}
	for _, pub := range view.Publications {
		if pub.VariantID != id {
			continue
		}
		if pub.Channel == "saby" {
			continue
		}
		expected := aWB
		if pub.Channel == "ozon" {
			expected = aOzon
		}
		if pub.Status != "created" || pub.ExternalIDs.OfferID != expected || pub.ExternalIDs.ProductID == before[pub.Channel] {
			t.Fatalf("replacement failed: %+v", pub)
		}
		old, e := mock.Executor.ReadEditorCardByArticle(ctx, pub.Channel, m.Article)
		if e != nil || old == nil || old.ExternalIDs.ProductID != before[pub.Channel] {
			t.Fatalf("old card lost: %+v %v", old, e)
		}
		if old.ExternalIDs.Barcode == pub.ExternalIDs.Barcode {
			t.Fatal("replacement reused old card barcode")
		}
	}
	// An unchanged Save retains the completed replacement receipt.
	if _, e = repo.SaveEditorPlan(ctx, actor, p.ID, plan, true); e != nil {
		t.Fatal(e)
	}
	var resetCount int
	pool.QueryRow(ctx, `SELECT COUNT(*) FROM catalog_editor_publications WHERE variant_id=$1 AND channel IN ('wb','ozon') AND status<>'created'`, id).Scan(&resetCount)
	if resetCount != 0 {
		t.Fatal("repeat replacement reset the receipt")
	}
	// Returning to a prior article binds that card instead of creating a third one.
	replacement.WB.Article = &m.Article
	plan.Variants[0].Marketplace = replacement
	if _, e = repo.SaveEditorPlan(ctx, actor, p.ID, plan, true); e != nil {
		t.Fatal(e)
	}
	drive(24)
	view, e = repo.ReadEditorPlan(ctx, p.ID)
	if e != nil {
		t.Fatal(e)
	}
	for _, pub := range view.Publications {
		if pub.VariantID != id {
			continue
		}
		if pub.Channel == "wb" && (!terminal(pub.Status) || pub.ExternalIDs.ProductID != before["wb"]) {
			t.Fatalf("prior card was not rebound: %+v", pub)
		}
		if pub.Channel == "ozon" && pub.ExternalIDs.OfferID != aOzon {
			t.Fatal("WB relink changed Ozon identity")
		}
	}
	var history int
	pool.QueryRow(ctx, `SELECT COUNT(*) FROM catalog_editor_link_history WHERE variant_id=$1`, id).Scan(&history)
	if history < 3 {
		t.Fatalf("missing link history: %d", history)
	}
	// A canonical owner blocks reassignment before the target receives updates.
	occupied := "occupied-" + tag
	request := frozen
	request.Marketplace = replacement
	request.Marketplace.Description = "Do not change this target"
	request.Channel = replacement.WB
	request.Channel.Article = &occupied
	request.Barcode = "2000000999910"
	raw, _ := mock.Executor.ReadEditorReference(ctx, "wb", "attributes", 900003, 0, 0, "")
	var schema struct {
		Data []ce.Attribute `json:"data"`
	}
	json.Unmarshal(raw, &schema)
	request.Schema = schema.Data
	state := ce.Progress{}
	for i := 0; i < 15; i++ {
		res, err := mock.Advance(ctx, "wb", request, state)
		if err != nil {
			t.Fatal(err)
		}
		state = res.Progress
		if res.Status == "created" {
			break
		}
	}
	if _, e = pool.Exec(ctx, `INSERT INTO product_external_ids(product_id,variant_id,provider,id_type,external_id,source) VALUES($1,$2,'wildberries','vendor_code',$3,'manual')`, p.ID, otherID, occupied); e != nil {
		t.Fatal(e)
	}
	replacement.WB.Article = &occupied
	plan.Variants[0].Marketplace = replacement
	if _, e = repo.SaveEditorPlan(ctx, actor, p.ID, plan, true); e != nil {
		t.Fatal(e)
	}
	drive(8)
	view, e = repo.ReadEditorPlan(ctx, p.ID)
	if e != nil {
		t.Fatal(e)
	}
	for _, pub := range view.Publications {
		if pub.VariantID != id {
			continue
		}
		if pub.Channel == "wb" && pub.Status != "failed" {
			t.Fatalf("occupied card accepted: %+v", pub)
		}
	}
	if view.Links[id]["wb"] != before["wb"] {
		t.Fatal("conflict destroyed previous link")
	}
	target, err := mock.Executor.ReadEditorCardByArticle(ctx, "wb", occupied)
	if err != nil || target.Card.Description != "Do not change this target" {
		t.Fatalf("occupied card was modified: %+v %v", target, err)
	}

}
