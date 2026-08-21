package admin

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// ListVariantMedia returns only media owned by one sellable SKU. Product-level
// media are deliberately not mixed in here; the PDP performs that fallback.
func (repository *PostgresRepository) ListVariantMedia(ctx context.Context, variantID int64) ([]ProductMedia, error) {
	rows, err := repository.pool.Query(ctx, `
		SELECT media.id, COALESCE(mirror.large_url, media.object_key),
			media.is_primary <> 0, media.sort_order
		FROM product_media media
		LEFT JOIN media_mirror mirror ON mirror.source_url = media.object_key
		WHERE media.variant_id = $1
		ORDER BY media.is_primary DESC, media.sort_order, media.id
	`, variantID)
	if err != nil {
		return nil, fmt.Errorf("query variant media: %w", err)
	}
	defer rows.Close()
	items := make([]ProductMedia, 0)
	for rows.Next() {
		var item ProductMedia
		if err := rows.Scan(&item.ID, &item.URL, &item.Primary, &item.SortOrder); err != nil {
			return nil, fmt.Errorf("scan variant media: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (repository *PostgresRepository) AddUploadedVariantMedia(
	ctx context.Context,
	actor Actor,
	variantID int64,
	sourceKey, cardURL, largeURL string,
) (ProductMedia, error) {
	if !Can(actor.Role, PermissionProductsEdit) {
		return ProductMedia{}, ErrForbidden
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return ProductMedia{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var productID int64
	var alt string
	if err := tx.QueryRow(ctx, `
		SELECT variant.product_id, product.name || CASE WHEN BTRIM(variant.label)='' THEN '' ELSE ' — ' || variant.label END
		FROM product_variants variant JOIN products product ON product.id=variant.product_id
		WHERE variant.id=$1 FOR UPDATE OF variant
	`, variantID).Scan(&productID, &alt); err != nil {
		return ProductMedia{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO media_mirror(source_url, card_url, large_url, attempts, failure, mirrored_at, checked_at)
		VALUES($1,$2,$3,0,'',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)
		ON CONFLICT(source_url) DO UPDATE SET card_url=EXCLUDED.card_url,
			large_url=EXCLUDED.large_url, failure='', mirrored_at=CURRENT_TIMESTAMP,
			checked_at=CURRENT_TIMESTAMP
	`, sourceKey, cardURL, largeURL); err != nil {
		return ProductMedia{}, fmt.Errorf("store uploaded variant media mapping: %w", err)
	}
	var id int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO product_media(product_id, variant_id, object_key, alt_text, sort_order, is_primary)
		SELECT $1,$2,$3,$4,COALESCE(MAX(sort_order),-1)+1,CASE WHEN COUNT(*)=0 THEN 1 ELSE 0 END
		FROM product_media WHERE variant_id=$2
		RETURNING id
	`, productID, variantID, sourceKey, alt).Scan(&id); err != nil {
		return ProductMedia{}, fmt.Errorf("add variant media: %w", err)
	}
	if err := insertAudit(ctx, tx, actor, "variant.media.add", "variant", fmt.Sprint(variantID), nil, map[string]any{"mediaId": id}); err != nil {
		return ProductMedia{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ProductMedia{}, err
	}
	items, err := repository.ListVariantMedia(ctx, variantID)
	if err != nil {
		return ProductMedia{}, err
	}
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
	}
	return ProductMedia{}, pgx.ErrNoRows
}

func (repository *PostgresRepository) DeleteVariantMedia(ctx context.Context, actor Actor, variantID, mediaID int64) error {
	if !Can(actor.Role, PermissionProductsEdit) {
		return ErrForbidden
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var source string
	var primary bool
	if err := tx.QueryRow(ctx, `
		SELECT object_key,is_primary<>0 FROM product_media
		WHERE id=$1 AND variant_id=$2 FOR UPDATE
	`, mediaID, variantID).Scan(&source, &primary); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "DELETE FROM product_media WHERE id=$1 AND variant_id=$2", mediaID, variantID); err != nil {
		return fmt.Errorf("delete variant media: %w", err)
	}
	if primary {
		if _, err := tx.Exec(ctx, `
			UPDATE product_media SET is_primary=1
			WHERE id=(SELECT id FROM product_media WHERE variant_id=$1 ORDER BY sort_order,id LIMIT 1)
		`, variantID); err != nil {
			return fmt.Errorf("promote next variant media: %w", err)
		}
	}
	if strings.HasPrefix(source, "upload://") {
		if _, err := tx.Exec(ctx, `DELETE FROM media_mirror WHERE source_url=$1
			AND NOT EXISTS(SELECT 1 FROM product_media WHERE object_key=$1)`, source); err != nil {
			return fmt.Errorf("clean variant media mapping: %w", err)
		}
	}
	if err := insertAudit(ctx, tx, actor, "variant.media.delete", "variant", fmt.Sprint(variantID), map[string]any{"mediaId": mediaID}, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (repository *PostgresRepository) SetPrimaryVariantMedia(ctx context.Context, actor Actor, variantID, mediaID int64) error {
	if !Can(actor.Role, PermissionProductsEdit) {
		return ErrForbidden
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM product_media WHERE id=$1 AND variant_id=$2)", mediaID, variantID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return pgx.ErrNoRows
	}
	if _, err := tx.Exec(ctx, "UPDATE product_media SET is_primary=CASE WHEN id=$2 THEN 1 ELSE 0 END WHERE variant_id=$1", variantID, mediaID); err != nil {
		return fmt.Errorf("set primary variant media: %w", err)
	}
	if err := insertAudit(ctx, tx, actor, "variant.media.primary", "variant", fmt.Sprint(variantID), nil, map[string]any{"mediaId": mediaID}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
