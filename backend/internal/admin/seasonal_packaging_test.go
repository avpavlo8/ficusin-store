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

func TestWinterPackageMeasurements(t *testing.T) {
	for _, code := range []string{"package_winter_length_cm", "package_winter_width_cm", "package_winter_height_cm"} {
		for _, value := range []any{0, -1, "20", false} {
			if err := validatePackageMeasurement(code, value); !errors.Is(err, ErrInvalidInput) {
				t.Errorf("%s accepted %v: %v", code, value, err)
			}
		}
		if err := validatePackageMeasurement(code, float64(20)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestManagerCanSaveWinterPackagingOnLiveDatabase(t *testing.T) {
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
	var productID, variantID, staffID int64
	suffix := fmt.Sprint(time.Now().UnixNano())
	if err = pool.QueryRow(ctx, `INSERT INTO products(name,slug,category_id,status) SELECT 'Seasonal packaging test',$1,id,'draft' FROM categories WHERE slug='plants' RETURNING id`, "seasonal-"+suffix).Scan(&productID); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, productID) }()
	if err = pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,label,is_active) VALUES($1,'Seasonal test',0) RETURNING id`, productID).Scan(&variantID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO customers(phone,password_hash,full_name,consent_at) VALUES($1,'','Seasonal manager',CURRENT_TIMESTAMP) RETURNING id`, "seasonal-manager-"+suffix).Scan(&staffID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM admin_audit_log WHERE actor_customer_id=$1`, staffID)
		_, _ = pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, staffID)
	}()
	repository := NewPostgresRepository(pool)
	result, err := repository.UpdateVariantContent(ctx, Actor{CustomerID: staffID, Role: RoleManager}, variantID, VariantContentUpdate{Attributes: map[string]any{"package_winter_length_cm": float64(40), "package_winter_width_cm": float64(20), "package_winter_height_cm": float64(20)}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Attributes["package_winter_width_cm"] != float64(20) {
		t.Fatalf("winter packaging not saved: %+v", result.Attributes)
	}
}
