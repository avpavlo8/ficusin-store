package admin

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// SetVariantMarketplaceScope records which channels the owner permits for a
// site SKU. Listing and price changes remain separate, explicit operations.
func (repository *PostgresRepository) SetVariantMarketplaceScope(ctx context.Context, actor Actor, variantID int64, scope string) error {
	if err := ownerOnly(actor); err != nil {
		return err
	}
	if variantID <= 0 || (scope != "unknown" && scope != "site_only" && scope != "marketplaces") {
		return ErrInvalidInput
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var oldScope, sku string
	err = tx.QueryRow(ctx, `SELECT marketplace_scope,sku FROM product_variants WHERE id=$1 AND archived_at IS NULL FOR UPDATE`, variantID).Scan(&oldScope, &sku)
	if err == pgx.ErrNoRows {
		return ErrInvalidInput
	}
	if err != nil {
		return err
	}
	if oldScope == scope {
		return tx.Commit(ctx)
	}
	if _, err = tx.Exec(ctx, `UPDATE product_variants SET marketplace_scope=$2,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, variantID, scope); err != nil {
		return err
	}
	if scope == "site_only" {
		if _, err = tx.Exec(ctx, `UPDATE catalog_editor_price_publications SET status='superseded',message='Маркетплейсы отключены для SKU на сайте',updated_at=NOW() WHERE variant_id=$1 AND channel IN('wb','ozon') AND (status='draft' OR(status='failed' AND COALESCE(progress->>'ambiguous','false')<>'true' AND COALESCE(progress->>'stage','') IN('','price-rejected')))`, variantID); err != nil {
			return err
		}
	}
	if err = insertAudit(ctx, tx, actor, "variant.marketplace_scope.change", "variant", fmt.Sprint(variantID),
		map[string]any{"sku": sku, "marketplaceScope": oldScope}, map[string]any{"sku": sku, "marketplaceScope": scope}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
