package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestEditorSabyLinksReadMirrorWithoutChangingIdentityOrSavedPlan(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var database string
	if err = pool.QueryRow(ctx, `SELECT current_database()`).Scan(&database); err != nil {
		t.Fatal(err)
	}
	if database != "ficusin_local_publisher_test" {
		t.Skip("requires isolated local publisher test database")
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	tag := fmt.Sprintf("editor-saby-%d", time.Now().UnixNano())
	var productID int64
	must(pool.QueryRow(ctx, `INSERT INTO products(name,slug,status) VALUES('Mirror fixture',$1,'draft') RETURNING id`, tag).Scan(&productID))
	defer func() {
		pool.Exec(ctx, `DELETE FROM catalog_editor_references WHERE channel='saby' AND scope LIKE $1`, tag+"%")
		pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, productID)
		pool.Exec(ctx, `DELETE FROM saby_nomenclature WHERE saby_id LIKE $1`, tag+"%")
		pool.Exec(ctx, `DELETE FROM saby_catalog_folders WHERE saby_id=$1`, tag+"-folder")
	}()
	exec := func(sql string, args ...any) { _, err := pool.Exec(ctx, sql, args...); must(err) }
	variant := func(id string) int64 {
		t.Helper()
		var variantID int64
		must(pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,label,base_price_minor,is_active,saby_id) VALUES($1,$2,10000,1,NULLIF($2,'')) RETURNING id`, productID, id).Scan(&variantID))
		return variantID
	}
	external := func(variantID int64, kind, value string) {
		exec(`INSERT INTO product_external_ids(product_id,variant_id,provider,id_type,external_id,source) VALUES($1,$2,'saby',$3,$4,'test')`, productID, variantID, kind, value)
	}
	mirror := func(id, code, folder string) {
		exec(`INSERT INTO saby_nomenclature(saby_id,code,name,folder_saby_id) VALUES($1,$2,'Mirror fixture',NULLIF($3,''))`, id, code, folder)
	}
	exec(`INSERT INTO saby_catalog_folders(saby_id,name,path) VALUES($1,'Растения',ARRAY['Каталог','Растения'])`, tag+"-folder")
	exact := variant(tag + "-exact")
	mirror(tag+"-exact", "X"+tag+"-exact", tag+"-folder")
	// Even a conflicting old code alias cannot replace an exact integration ID.
	external(exact, "code", "X"+tag+"-stale")
	mirror(tag+"-other", "X"+tag+"-stale", "")
	legacy := variant(tag + "-numeric-id")
	external(legacy, "code", "X"+tag+"-legacy")
	uuid := fmt.Sprintf("00000000-0000-0000-0000-%012d", productID)
	external(legacy, "alias", uuid)
	mirror(tag+"-legacy-code", "X"+tag+"-legacy", tag+"-folder")
	alias := variant(tag + "-alias-id")
	external(alias, "alias", tag+"-uuid")
	mirror(tag+"-uuid", "X"+tag+"-alias", "")
	ambiguous := variant(tag + "-ambiguous-id")
	external(ambiguous, "code", "X"+tag+"-ambiguous")
	mirror(tag+"-ambiguous-a", "X"+tag+"-ambiguous", tag+"-folder")
	mirror(tag+"-ambiguous-b", "X"+tag+"-ambiguous", "")
	missing := variant(tag + "-missing-id")
	external(missing, "code", "X"+tag+"-missing")
	unlinked := variant("")
	plan := EditorPlan{Variants: []EditorVariantPlan{{VariantID: exact, Saby: EditorSaby{Mode: "create", ExistingCode: "draft-code", Category: EditorCategory{ID: "draft-category", Name: "Черновик категории"}, Folder: EditorCategory{ID: "draft-folder", Name: "Черновик раздела"}}}}}
	exec(`INSERT INTO catalog_editor_plans(product_id,config) VALUES($1,$2)`, productID, plan)
	var before string
	must(pool.QueryRow(ctx, `SELECT config::text FROM catalog_editor_plans WHERE product_id=$1`, productID).Scan(&before))
	repo := NewPostgresRepository(pool)
	for range 2 {
		view, e := repo.ReadEditorPlan(ctx, productID)
		must(e)
		if !reflect.DeepEqual(view.Config, plan) {
			t.Fatalf("read replaced editable saved choices: %+v", view.Config)
		}
		for id, want := range map[int64]EditorSabyLink{
			exact:     {ID: tag + "-exact", Code: "X" + tag + "-exact", Folder: EditorCategory{ID: tag + "-folder", Name: "Каталог / Растения"}},
			legacy:    {ID: tag + "-numeric-id", Code: "X" + tag + "-legacy", Folder: EditorCategory{ID: tag + "-folder", Name: "Каталог / Растения"}},
			alias:     {ID: tag + "-alias-id", Code: "X" + tag + "-alias"},
			ambiguous: {ID: tag + "-ambiguous-id", Code: "X" + tag + "-ambiguous"},
			missing:   {ID: tag + "-missing-id", Code: "X" + tag + "-missing"},
		} {
			if got := view.SabyLinks[id]; got != want {
				t.Fatalf("variant %d: got %+v, want %+v", id, got, want)
			}
		}
		if _, ok := view.SabyLinks[unlinked]; ok || len(view.SabyLinks) != 5 {
			t.Fatalf("unlinked variant was hydrated: %+v", view.SabyLinks)
		}
	}
	verified := catalogeditor.SabyMetadata{ID: tag + "-numeric-id", Code: "X" + tag + "-legacy", UUID: uuid, Category: EditorCategory{ID: "1041", Name: "Комнатные растения"}, Folder: EditorCategory{ID: "3333", Name: "Цветы маркетплейс"}}
	cache := func(kind string, metadata catalogeditor.SabyMetadata) {
		raw, e := json.Marshal(metadata)
		must(e)
		must(repo.CacheEditorReference(ctx, "saby", kind, metadata.ID, raw))
	}
	cache("linked-card", verified)
	repo.WithEditorPublicationMode("mock")
	view, e := repo.ReadEditorPlan(ctx, productID)
	must(e)
	if view.SabyLinks[legacy].Category.ID != "" {
		t.Fatal("mock read leaked a live metadata cache")
	}
	cache("linked-card", verified)
	for range 2 {
		view, e = repo.ReadEditorPlan(ctx, productID)
		must(e)
		if view.SabyLinks[legacy].Category != verified.Category || view.SabyLinks[legacy].Folder != verified.Folder || view.SabyLinks[legacy].ID != tag+"-numeric-id" {
			t.Fatalf("verified readback cache did not hydrate linked metadata: %+v", view.SabyLinks[legacy])
		}
	}
	bad := verified
	bad.UUID = "00000000-0000-0000-0000-000000000000"
	cache("linked-card", bad)
	view, e = repo.ReadEditorPlan(ctx, productID)
	must(e)
	if view.SabyLinks[legacy].Category.ID != "" {
		t.Fatal("mismatched readback identity contaminated editor")
	}
	partial := verified
	partial.Category.Name, partial.Folder.Name = "", ""
	cache("linked-card", partial)
	reader := &editorSabyMetadataFixture{metadata: partial, pool: pool}
	repo.WithEditorSabyMetadata(reader, "development")
	view, e = repo.ReadEditorPlan(ctx, productID)
	must(e)
	if view.SabyLinks[legacy].Category.ID != "" || view.SabyLinks[legacy].Folder.Name != "Каталог / Растения" {
		t.Fatal("unnamed typed identifiers poisoned editable hydration or replaced a known folder")
	}
	reader.metadata, reader.targetReads = verified, 0
	for range 2 {
		view, e = repo.ReadEditorPlan(ctx, productID)
		must(e)
		if view.SabyLinks[legacy].Category != verified.Category {
			t.Fatal("configured read-only source did not hydrate metadata")
		}
	}
	if reader.targetReads != 1 {
		t.Fatalf("expected one read then cached response, got %d", reader.targetReads)
	}
	exec(`UPDATE catalog_editor_references SET fetched_at=NOW()-INTERVAL '3 days' WHERE channel='saby' AND kind='mock/linked-card' AND scope=$1`, verified.ID)
	repo.WithEditorSabyMetadata(nil, "staging")
	view, e = repo.ReadEditorPlan(ctx, productID)
	must(e)
	if view.SabyLinks[legacy].Category != verified.Category {
		t.Fatal("verified offline staging snapshot disappeared after normal reference TTL")
	}
	repo.WithEditorSabyMetadata(nil, "production")
	view, e = repo.ReadEditorPlan(ctx, productID)
	must(e)
	if view.SabyLinks[legacy].Category.ID != "" {
		t.Fatal("stale preview snapshot bypassed production TTL")
	}
	var after, legacyID string
	must(pool.QueryRow(ctx, `SELECT config::text FROM catalog_editor_plans WHERE product_id=$1`, productID).Scan(&after))
	must(pool.QueryRow(ctx, `SELECT saby_id FROM product_variants WHERE id=$1`, legacy).Scan(&legacyID))
	if after != before || legacyID != tag+"-numeric-id" {
		t.Fatal("read-only hydration mutated plan or numeric integration identity")
	}
}

type editorSabyMetadataFixture struct {
	metadata    catalogeditor.SabyMetadata
	targetReads int
	pool        *pgxpool.Pool
}

func (*editorSabyMetadataFixture) Configured() bool { return true }
func (f *editorSabyMetadataFixture) ReadSabyEditorMetadata(ctx context.Context, identity catalogeditor.SabyIdentity) (catalogeditor.SabyMetadata, error) {
	if identity.ID != f.metadata.ID {
		return catalogeditor.SabyMetadata{}, errors.New("fixture unavailable")
	}
	f.targetReads++
	if err := f.pool.Ping(ctx); err != nil {
		return catalogeditor.SabyMetadata{}, err
	}
	return f.metadata, nil
}
