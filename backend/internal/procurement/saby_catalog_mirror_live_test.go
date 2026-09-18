package procurement

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStage13SabyCatalogueMirrorIncludesUnimportedProductsOnLiveDatabase(t *testing.T) {
	dsn := os.Getenv("CRM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("CRM_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	var supplierID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM procurement_suppliers WHERE active ORDER BY id LIMIT 1`).Scan(&supplierID); err != nil {
		t.Fatal(err)
	}

	unique := time.Now().UnixNano()
	folderID := fmt.Sprintf("ci-folder-%d", unique)
	sabyID := fmt.Sprintf("ci-saby-%d", unique)
	code := fmt.Sprintf("XCI%d", unique)
	name := "Антуриум Блэк Бьюти CI"

	if _, err := pool.Exec(ctx, `
		INSERT INTO saby_catalog_folders(saby_id,parent_saby_id,name,path,seen_at,missing_since)
		VALUES($1,NULL,'Цветы маркетплейс',ARRAY['Цветы маркетплейс'],CURRENT_TIMESTAMP,NULL)
	`, folderID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM saby_nomenclature WHERE saby_id=$1`, sabyID)
		_, _ = pool.Exec(ctx, `DELETE FROM saby_catalog_folders WHERE saby_id=$1`, folderID)
	}()

	if _, err := pool.Exec(ctx, `
		INSERT INTO saby_nomenclature(
			saby_id,code,name,price_minor,balance,section_path,folder_saby_id,seen_at,missing_since
		) VALUES($1,$2,$3,395000,4,ARRAY['Цветы маркетплейс'],$4,CURRENT_TIMESTAMP,NULL)
	`, sabyID, code, name, folderID); err != nil {
		t.Fatal(err)
	}

	store := NewPostgresStore(pool)
	items, err := store.ListProducts(ctx, supplierID, code)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("items=%d, want 1: %+v", len(items), items)
	}
	item := items[0]
	if item.SabyID != sabyID || item.SabyCode != code || item.Name != name {
		t.Fatalf("wrong mirror item: %+v", item)
	}
	if item.VariantID != 0 || item.SiteStatus != "" {
		t.Fatalf("unimported Saby item pretended to exist on site: %+v", item)
	}
	if item.FolderID != folderID || len(item.SectionPath) != 1 || item.SectionPath[0] != "Цветы маркетплейс" {
		t.Fatalf("folder identity lost: %+v", item)
	}
	if item.Balance != 4 || item.CurrentPriceRUB != 3950 {
		t.Fatalf("Saby state lost: %+v", item)
	}

	folders, err := store.ListSabyCatalogFolders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, folder := range folders {
		if folder.ID == folderID {
			found = folder.Name == "Цветы маркетплейс" && len(folder.Path) == 1
			break
		}
	}
	if !found {
		t.Fatalf("folder %s missing from mirror: %+v", folderID, folders)
	}
}
