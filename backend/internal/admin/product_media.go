package admin

import (
	"context"
	"fmt"
)

// ProductMedia is the manager-facing view of one PDP image. URL is already
// resolved to our mirrored/uploaded large image when one exists.
type ProductMedia struct {
	ID        int64  `json:"id"`
	URL       string `json:"url"`
	Primary   bool   `json:"primary"`
	SortOrder int    `json:"sortOrder"`
}

func (repository *PostgresRepository) ListProductMedia(ctx context.Context, productID int64) ([]ProductMedia, error) {
	rows, err := repository.pool.Query(ctx, `
		SELECT media.id, COALESCE(mirror.large_url, media.object_key),
			media.is_primary <> 0, media.sort_order
		FROM product_media media
		LEFT JOIN media_mirror mirror ON mirror.source_url = media.object_key
		WHERE media.product_id = $1
		ORDER BY media.is_primary DESC, media.sort_order, media.id
	`, productID)
	if err != nil {
		return nil, fmt.Errorf("query product media: %w", err)
	}
	defer rows.Close()
	result := make([]ProductMedia, 0)
	for rows.Next() {
		var item ProductMedia
		if err := rows.Scan(&item.ID, &item.URL, &item.Primary, &item.SortOrder); err != nil {
			return nil, fmt.Errorf("scan product media: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// AddUploadedProductMedia links two already prepared S3 sizes to a product.
// sourceKey is intentionally not an HTTP URL (upload://...): the background
// Saby mirror therefore never downloads and recompresses our own upload.
func (repository *PostgresRepository) AddUploadedProductMedia(
	ctx context.Context,
	actor Actor,
	productID int64,
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

	var productName string
	if err := tx.QueryRow(ctx, "SELECT name FROM products WHERE id=$1 FOR UPDATE", productID).Scan(&productName); err != nil {
		return ProductMedia{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO media_mirror(source_url, card_url, large_url, attempts, failure, mirrored_at, checked_at)
		VALUES($1,$2,$3,0,'',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)
		ON CONFLICT(source_url) DO UPDATE SET card_url=EXCLUDED.card_url,
			large_url=EXCLUDED.large_url, failure='', mirrored_at=CURRENT_TIMESTAMP,
			checked_at=CURRENT_TIMESTAMP
	`, sourceKey, cardURL, largeURL); err != nil {
		return ProductMedia{}, fmt.Errorf("store uploaded media mapping: %w", err)
	}
	var id int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO product_media(product_id, object_key, alt_text, sort_order, is_primary)
		SELECT $1,$2,$3,COALESCE(MAX(sort_order),-1)+1,
			CASE WHEN COUNT(*)=0 THEN 1 ELSE 0 END
		FROM product_media WHERE product_id=$1
		RETURNING id
	`, productID, sourceKey, productName).Scan(&id); err != nil {
		return ProductMedia{}, fmt.Errorf("add product media: %w", err)
	}
	if err := insertAudit(ctx, tx, actor, "product.media.add", "product", fmt.Sprint(productID), nil,
		map[string]any{"mediaId": id}); err != nil {
		return ProductMedia{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ProductMedia{}, err
	}
	items, err := repository.ListProductMedia(ctx, productID)
	if err != nil {
		return ProductMedia{}, err
	}
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
	}
	return ProductMedia{}, fmt.Errorf("uploaded media %d disappeared", id)
}

func (repository *PostgresRepository) DeleteProductMedia(ctx context.Context, actor Actor, productID, mediaID int64) error {
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
		SELECT object_key, is_primary <> 0 FROM product_media
		WHERE id=$1 AND product_id=$2 FOR UPDATE
	`, mediaID, productID).Scan(&source, &primary); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "DELETE FROM product_media WHERE id=$1 AND product_id=$2", mediaID, productID); err != nil {
		return fmt.Errorf("delete product media: %w", err)
	}
	if primary {
		if _, err := tx.Exec(ctx, `
			UPDATE product_media SET is_primary=1
			WHERE id=(SELECT id FROM product_media WHERE product_id=$1 ORDER BY sort_order,id LIMIT 1)
		`, productID); err != nil {
			return fmt.Errorf("promote next product media: %w", err)
		}
	}
	// Uploaded mappings are unique to this card. Supplier mappings can be
	// shared/reused by a later sync and must stay cached.
	if len(source) >= len("upload://") && source[:len("upload://")] == "upload://" {
		if _, err := tx.Exec(ctx, `DELETE FROM media_mirror WHERE source_url=$1
			AND NOT EXISTS(SELECT 1 FROM product_media WHERE object_key=$1)`, source); err != nil {
			return fmt.Errorf("clean uploaded media mapping: %w", err)
		}
	}
	if err := insertAudit(ctx, tx, actor, "product.media.delete", "product", fmt.Sprint(productID),
		map[string]any{"mediaId": mediaID}, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (repository *PostgresRepository) SetPrimaryProductMedia(ctx context.Context, actor Actor, productID, mediaID int64) error {
	if !Can(actor.Role, PermissionProductsEdit) {
		return ErrForbidden
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM product_media WHERE id=$1 AND product_id=$2)", mediaID, productID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("media not found")
	}
	if _, err := tx.Exec(ctx, "UPDATE product_media SET is_primary=CASE WHEN id=$2 THEN 1 ELSE 0 END WHERE product_id=$1", productID, mediaID); err != nil {
		return fmt.Errorf("set primary product media: %w", err)
	}
	if err := insertAudit(ctx, tx, actor, "product.media.primary", "product", fmt.Sprint(productID), nil,
		map[string]any{"mediaId": mediaID}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
