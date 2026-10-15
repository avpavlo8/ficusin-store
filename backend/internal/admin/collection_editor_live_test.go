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

func TestCollectionEditorAtomicSaveOnLiveDatabase(t *testing.T) {
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
	var actorID int64
	if err := pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at) VALUES($1,$2,'','Collection test',NOW()) RETURNING id`, fmt.Sprintf("collection-%d@example.invalid", unique), fmt.Sprintf("+798%09d", unique%1000000000)).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	actor := Actor{CustomerID: actorID, Role: RoleOwner}
	repo := NewPostgresRepository(pool)
	product, err := repo.CreateProduct(ctx, actor, ProductCreate{Name: fmt.Sprintf("Collection product %d", unique), CatalogSection: "plants"})
	if err != nil {
		t.Fatal(err)
	}
	input := CollectionDefinitionInput{Title: "Original", Slug: fmt.Sprintf("test-%d", unique), Mode: "manual", CoverURL: "/assets/hero-monstera.webp"}
	item, err := repo.CreateCollectionDefinition(ctx, actor, input)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM collections WHERE id=$1", item.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM admin_audit_log WHERE actor_customer_id=$1", actorID)
		_, _ = pool.Exec(ctx, "DELETE FROM products WHERE id=$1", product.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM customers WHERE id=$1", actorID)
	}()
	members := []int64{product.ID}
	input.Products = &members
	input.Title = "Saved together"
	saved, err := repo.UpdateCollectionDefinition(ctx, actor, item.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Title != input.Title || len(saved.Products) != 1 || saved.Products[0] != product.ID {
		t.Fatalf("settings and membership diverged: %+v", saved)
	}
	invalid := []int64{product.ID, -1}
	input.Products = &invalid
	input.Title = "Must roll back"
	if _, err := repo.UpdateCollectionDefinition(ctx, actor, item.ID, input); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid product: %v", err)
	}
	items, err := repo.ListCollectionDefinitions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, current := range items {
		if current.ID == item.ID && (current.Title != saved.Title || len(current.Products) != 1 || current.Products[0] != product.ID) {
			t.Fatalf("partial save escaped rollback: %+v", current)
		}
	}
	empty := []int64{}
	input.Title = "Empty membership"
	input.Products = &empty
	saved, err = repo.UpdateCollectionDefinition(ctx, actor, item.ID, input)
	if err != nil || len(saved.Products) != 0 {
		t.Fatalf("explicit empty list did not clear membership: %+v %v", saved, err)
	}
}
