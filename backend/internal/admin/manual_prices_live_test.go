package admin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestManualPriceProposalsAndStaleApprovalOnLiveDatabase(t *testing.T) {
	databaseURL := os.Getenv("CRM_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("CRM_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var variantID, oldPrice, actorID int64
	err = pool.QueryRow(ctx, `SELECT v.id,v.base_price_minor,c.id FROM product_variants v CROSS JOIN customers c ORDER BY v.id,c.id LIMIT 1`).Scan(&variantID, &oldPrice, &actorID)
	if errors.Is(err, pgx.ErrNoRows) {
		t.Skip("no variant and customer in test database")
	}
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err = createManualPriceProposals(ctx, tx, Actor{CustomerID: actorID}, variantID, oldPrice, oldPrice+10000); err != nil {
		t.Fatal(err)
	}
	var batchID int64
	var sitePrice int64
	if err = tx.QueryRow(ctx, `SELECT id,manual_site_price_minor FROM procurement_action_batches WHERE source='manual' AND manual_variant_id=$1 ORDER BY id DESC LIMIT 1`, variantID).Scan(&batchID, &sitePrice); err != nil {
		t.Fatal(err)
	}
	if sitePrice != oldPrice+10000 {
		t.Fatalf("stored site price = %d", sitePrice)
	}
	var proposalID int64
	if err = tx.QueryRow(ctx, `SELECT id FROM procurement_action_items WHERE batch_id=$1 AND channel='wb'`, batchID).Scan(&proposalID); err != nil {
		t.Fatal(err)
	}
	// The edit is intentionally rolled back; an approval for its proposed price
	// must therefore reject the stale snapshot of the live site price.
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM procurement_action_batches WHERE id=$1`, batchID) //nolint:errcheck
	repository := NewPostgresRepository(pool).WithPriceChannelsConfigured(map[string]bool{"wb": true})
	_, err = repository.ApproveManualPriceProposal(ctx, Actor{CustomerID: actorID, Role: RoleOwner}, proposalID)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("stale approval error = %v", err)
	}
}

func TestManualSabyExportRejectsSupersededPrice(t *testing.T) {
	databaseURL := os.Getenv("CRM_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("CRM_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	unique := time.Now().UnixNano()
	var actorID, productID, variantID, proposalID int64
	sabyID := fmt.Sprintf("manual-price-%d", unique)
	if err = pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at)
		VALUES($1,$2,'test','Price test',now()) RETURNING id`,
		fmt.Sprintf("manual-price-%d@example.invalid", unique), fmt.Sprintf("+79%09d", unique%1000000000)).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, actorID) }()
	if err = pool.QueryRow(ctx, `INSERT INTO products(name,slug,status) VALUES('Manual price test',$1,'draft') RETURNING id`, sabyID).Scan(&productID); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, productID) }()
	if _, err = pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,name) VALUES($1,'TEST-PRICE','Manual price test')`, sabyID); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM saby_nomenclature WHERE saby_id=$1`, sabyID) }()
	if err = pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,saby_id,sku,label,base_price_minor)
		VALUES($1,$2,$3,'D12',150000) RETURNING id`, productID, sabyID,
		fmt.Sprintf("9%017d", unique%100000000000000000)).Scan(&variantID); err != nil {
		t.Fatal(err)
	}
	actor := Actor{CustomerID: actorID, Role: RoleOwner}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = createManualPriceProposals(ctx, tx, actor, variantID, 100000, 150000); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT item.id FROM procurement_action_items item
		JOIN procurement_action_batches batch ON batch.id=item.batch_id
		WHERE batch.manual_variant_id=$1 AND item.channel='saby_price'`, variantID).Scan(&proposalID); err != nil {
		t.Fatal(err)
	}
	repository := NewPostgresRepository(pool)
	if _, err = repository.ApproveManualPriceProposal(ctx, actor, proposalID); err != nil {
		t.Fatal(err)
	}
	if _, _, err = repository.ManualSabyPriceXLSX(ctx, proposalID); err != nil {
		t.Fatalf("current approved price must export: %v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE product_variants SET base_price_minor=200000 WHERE id=$1`, variantID); err != nil {
		t.Fatal(err)
	}
	if _, _, err = repository.ManualSabyPriceXLSX(ctx, proposalID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("stale Saby price exported: %v", err)
	}
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = createManualPriceProposals(ctx, tx, actor, variantID, 150000, 200000); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var status string
	if err = pool.QueryRow(ctx, `SELECT status FROM procurement_action_items WHERE id=$1`, proposalID).Scan(&status); err != nil || status != "skipped" {
		t.Fatalf("old approved Saby proposal status=%q err=%v", status, err)
	}
}
