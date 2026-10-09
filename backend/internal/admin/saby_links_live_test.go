package admin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestLinkVariantToSabyOnLiveDatabase(t *testing.T) {
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
	unique := time.Now().UnixNano()
	sabyID := fmt.Sprintf("manual-link-%d", unique)
	code := fmt.Sprintf("X%d", unique)
	var productID, variantID int64
	defer func() {
		if productID != 0 {
			_, _ = pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, productID)
		}
		_, _ = pool.Exec(ctx, `DELETE FROM saby_nomenclature WHERE saby_id=$1`, sabyID)
	}()
	if _, err = pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,name) VALUES($1,$2,'Тестовый фикус')`, sabyID, code); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO products(name,slug,status,saby_fields) VALUES('Тестовый фикус',$1,'draft',ARRAY[]::TEXT[]) RETURNING id`, fmt.Sprintf("manual-link-%d", unique)).Scan(&productID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,sku,label,base_price_minor,is_active) VALUES($1,$2,'12 см',100000,1) RETURNING id`, productID, fmt.Sprintf("9%017d", unique%100000000000000000)).Scan(&variantID); err != nil {
		t.Fatal(err)
	}
	// A historical local count must not turn payable when an identity is attached.
	_, err = pool.Exec(ctx, `INSERT INTO inventory(warehouse_id,variant_id,available_qty,reserved_qty,synced_at)
		SELECT id,$1,7,0,CURRENT_TIMESTAMP FROM warehouses WHERE saby_id='saby-ryazan-main' LIMIT 1`, variantID)
	if err != nil {
		t.Fatal(err)
	}
	repository := NewPostgresRepository(pool)
	items, err := repository.SearchSabyLinkCandidates(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range items {
		if item.ID == sabyID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Saby candidate %q absent", sabyID)
	}
	if err = repository.LinkVariantToSaby(ctx, Actor{Role: RoleManager}, variantID, sabyID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("manager link: %v", err)
	}
	if err = repository.LinkVariantToSaby(ctx, Actor{Role: RoleOwner}, variantID, sabyID); err != nil {
		t.Fatal(err)
	}
	var variantSabyID, productSabyID string
	var qty int
	var fresh bool
	if err = pool.QueryRow(ctx, `SELECT v.saby_id,p.saby_id,COALESCE((SELECT available_qty FROM inventory WHERE variant_id=v.id LIMIT 1),0),
		EXISTS(SELECT 1 FROM inventory WHERE variant_id=v.id AND synced_at>now()-interval '2 hours')
		FROM product_variants v JOIN products p ON p.id=v.product_id WHERE v.id=$1`, variantID).Scan(&variantSabyID, &productSabyID, &qty, &fresh); err != nil {
		t.Fatal(err)
	}
	if variantSabyID != sabyID || productSabyID != sabyID || qty != 0 || fresh {
		t.Fatalf("link=%s/%s qty=%d fresh=%v", variantSabyID, productSabyID, qty, fresh)
	}
	if err = repository.LinkVariantToSaby(ctx, Actor{Role: RoleOwner}, variantID, sabyID); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("relink must fail: %v", err)
	}
}
