package admin

import (
	"context"
	"errors"
	"os"
	"testing"

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
