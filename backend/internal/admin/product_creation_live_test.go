package admin

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/migrate"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestProductCreationAtomicAndIdempotentOnLiveDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	root, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	// Never copy customer data: create a fresh, explicitly staging-named DB.
	name := fmt.Sprintf("ficusin_staging_creation_%d", time.Now().UnixNano())
	if _, err = root.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = root.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)") }()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = migrate.Apply(ctx, pool, "../../../timeweb/migrations"); err != nil {
		t.Fatal(err)
	}
	var customer, category int64
	if err = pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at) VALUES('creation@example.invalid','+79995550000','','Creation test',NOW()) RETURNING id`).Scan(&customer); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT id FROM categories WHERE slug='plants'`).Scan(&category); err != nil {
		t.Fatal(err)
	}
	repo := NewPostgresRepository(pool)
	actor := Actor{CustomerID: customer, Role: RoleOwner}
	draft, err := repo.NewProductCreation(ctx, actor)
	if err != nil {
		t.Fatal(err)
	}
	if draft.Data.WholesaleMinQty != 1 {
		t.Fatalf("new draft minimum quantity %d, want 1", draft.Data.WholesaleMinQty)
	}

	again, err := repo.NewProductCreation(ctx, actor)
	if err != nil || again.ID != draft.ID {
		t.Fatalf("resume %+v %v", again, err)
	}
	if _, err = repo.NewProductCreation(ctx, Actor{CustomerID: customer, Role: RoleManager}); err == nil {
		t.Fatal("manager created marketplace draft")
	}
	if err = NewPostgresRepository(root).creationAccess(ctx, actor); err == nil {
		t.Fatal("creation allowed in non-staging database")
	}
	var imageBytes bytes.Buffer
	if err = jpeg.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 4, 4)), nil); err != nil {
		t.Fatal(err)
	}
	draft, err = repo.AddProductCreationPhoto(ctx, actor, draft.ID, "plant.jpg", imageBytes.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	input := validCreationTestInput()
	input.CategoryID = category
	input.Photos = []string{draft.Photos[0].ID}
	input.Step = 6
	input.Attributes = map[string]any{}
	input.VariantAttributes = map[string]any{}
	schema, err := repo.EffectiveCategoryAttributes(ctx, category)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range schema {
		if !field.Required || field.Excluded {
			continue
		}
		var value any = "Тестовое значение"
		switch field.DataType {
		case "boolean":
			value = false
		case "number":
			value = float64(10)
		case "enum", "multi_enum":
			for _, option := range field.Options {
				if option.Active {
					value = option.Code
					if field.DataType == "multi_enum" {
						value = []string{option.Code}
					}
					break
				}
			}
		}
		if field.Scope == "variant" {
			input.VariantAttributes[field.Code] = value
		} else {
			input.Attributes[field.Code] = value
		}
	}
	wb, ozon := creationSchemas()
	input.PackageLengthCM = 20
	input.PackageWidthCM = 20
	input.PackageHeightCM = 40
	input.PackageWeightGrams = 700
	input.WB = CreationChannel{Enabled: true, Category: wb[0].ID, PriceMinor: 200000, Values: map[string]string{"brand": "Тест", "plantKind": "Аглаонема", "country": "Нидерланды", "contents": "Растение в горшке"}}
	input.Ozon = CreationChannel{Enabled: true, Category: ozon[0].ID, PriceMinor: 300000, Values: map[string]string{"brand": "Тест", "plantKind": "Аглаонема", "country": "Нидерланды", "contents": "Растение в горшке", "tnved": "0000000000", "vat": "Тест"}}
	draft, err = repo.SaveProductCreation(ctx, actor, draft.ID, draft.Revision, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.SaveProductCreation(ctx, actor, draft.ID, draft.Revision-1, input); err == nil {
		t.Fatal("stale draft silently overwrote current revision")
	}
	var results [2]CreationDraft
	var failures [2]error
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], failures[i] = repo.FinishProductCreation(ctx, actor, draft.ID, draft.Revision)
		}(i)
	}
	wg.Wait()
	for _, err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if results[0].Product == nil || results[1].Product == nil || results[0].Product.ID != results[1].Product.ID {
		t.Fatal("duplicate final POST did not return the same product")
	}
	product := results[0].Product
	if product.Status != "draft" || product.Stock != 0 {
		t.Fatalf("unexpected visibility/stock %+v", product)
	}
	if len(results[0].Results) != 3 {
		t.Fatalf("missing test channel cards %+v", results[0].Results)
	}
	if _, err := repo.DeleteDraftProducts(ctx, actor, []int64{product.ID}); err == nil {
		t.Fatal("created external-channel history must require archival instead of draft deletion")
	}

	media, err := repo.ListProductMedia(ctx, product.ID)
	if err != nil || len(media) != 1 || media[0].URL != "/api/v1/product-creation-photos/"+input.Photos[0] {
		t.Fatalf("photo %v %v", media, err)
	}
	variants, err := repo.ListProductVariants(ctx, product.ID)
	if err != nil || len(variants) != 1 || variants[0].WholesaleMinQty != input.WholesaleMinQty {
		t.Fatalf("variant %v %v", variants, err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM product_creation_staging_cards WHERE draft_id=$1`, draft.ID).Scan(&count); err != nil || count != 3 {
		t.Fatalf("non-idempotent channel copies %d %v", count, err)
	}
}
