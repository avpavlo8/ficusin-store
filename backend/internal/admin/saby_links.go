package admin

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

type SabyLinkCandidate struct {
	ID              string `json:"id"`
	Code            string `json:"code"`
	Article         string `json:"article"`
	Name            string `json:"name"`
	Missing         bool   `json:"missing"`
	LinkedVariantID *int64 `json:"linkedVariantId,omitempty"`
}

// SearchSabyLinkCandidates reads the last imported Saby directory. The search
// does not contact Saby or change catalogue data.
func (repository *PostgresRepository) SearchSabyLinkCandidates(ctx context.Context, query string) ([]SabyLinkCandidate, error) {
	query = strings.TrimSpace(query)
	if len([]rune(query)) < 2 || len([]rune(query)) > 100 {
		return nil, fmt.Errorf("%w: введите от 2 до 100 символов", ErrInvalidInput)
	}
	rows, err := repository.pool.Query(ctx, `SELECT n.saby_id,n.code,n.article,n.name,n.missing_since IS NOT NULL,v.id
		FROM saby_nomenclature n LEFT JOIN product_variants v ON v.saby_id=n.saby_id
		WHERE n.saby_id ILIKE '%' || $1 || '%' OR n.code ILIKE '%' || $1 || '%'
		   OR n.article ILIKE '%' || $1 || '%' OR n.name ILIKE '%' || $1 || '%'
		ORDER BY (UPPER(n.code)=UPPER($1)) DESC,(n.saby_id=$1) DESC,(n.missing_since IS NULL) DESC,n.name,n.saby_id LIMIT 30`, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]SabyLinkCandidate, 0)
	for rows.Next() {
		var item SabyLinkCandidate
		if err := rows.Scan(&item.ID, &item.Code, &item.Article, &item.Name, &item.Missing, &item.LinkedVariantID); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// LinkVariantToSaby attaches only a previously unlinked site SKU to an
// existing, current Saby entry. Stock becomes usable only after a real sync.
func (repository *PostgresRepository) LinkVariantToSaby(ctx context.Context, actor Actor, variantID int64, sabyID string) error {
	if actor.Role != RoleOwner {
		return ErrForbidden
	}
	sabyID = strings.TrimSpace(sabyID)
	if variantID < 1 || sabyID == "" {
		return fmt.Errorf("%w: укажите вариант и запись СБИС", ErrInvalidInput)
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var productID int64
	var currentID string
	var archived bool
	if err = tx.QueryRow(ctx, `SELECT product_id,COALESCE(saby_id,''),archived_at IS NOT NULL FROM product_variants WHERE id=$1 FOR UPDATE`, variantID).Scan(&productID, &currentID, &archived); err != nil {
		return err
	}
	if archived || currentID != "" {
		return fmt.Errorf("%w: вариант уже связан или архивирован; перепривязка запрещена", ErrInvalidInput)
	}
	var missing bool
	if err = tx.QueryRow(ctx, `SELECT missing_since IS NOT NULL FROM saby_nomenclature WHERE saby_id=$1 FOR UPDATE`, sabyID).Scan(&missing); err != nil {
		return err
	}
	if missing {
		return fmt.Errorf("%w: запись СБИС отсутствует в последней выгрузке", ErrInvalidInput)
	}
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM products WHERE saby_id=$1 AND id<>$2)
		OR EXISTS(SELECT 1 FROM product_variants WHERE saby_id=$1)
		OR EXISTS(SELECT 1 FROM product_external_ids WHERE provider='saby' AND id_type='id' AND external_id=$1 AND product_id<>$2)`, sabyID, productID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("%w: запись СБИС уже связана с другим товаром или вариантом", ErrInvalidInput)
	}
	var productSabyID string
	if err = tx.QueryRow(ctx, `SELECT COALESCE(saby_id,'') FROM products WHERE id=$1 FOR UPDATE`, productID).Scan(&productSabyID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE product_variants SET saby_id=$2,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, variantID, sabyID); err != nil {
		return err
	}
	if productSabyID == "" {
		if _, err = tx.Exec(ctx, `UPDATE products SET saby_id=$2,saby_fields=ARRAY['stock']::TEXT[],updated_at=CURRENT_TIMESTAMP WHERE id=$1`, productID, sabyID); err != nil {
			return err
		}
	}
	// Preserve a product-level legacy row if present; attach it to this SKU.
	var mappingVariantID *int64
	err = tx.QueryRow(ctx, `SELECT variant_id FROM product_external_ids WHERE provider='saby' AND id_type='id' AND external_id=$1 FOR UPDATE`, sabyID).Scan(&mappingVariantID)
	if err == nil {
		if mappingVariantID != nil {
			return fmt.Errorf("%w: запись СБИС уже связана с вариантом", ErrInvalidInput)
		}
		_, err = tx.Exec(ctx, `UPDATE product_external_ids SET variant_id=$2,status='active',updated_at=CURRENT_TIMESTAMP WHERE provider='saby' AND id_type='id' AND external_id=$1`, sabyID, variantID)
	} else if err == pgx.ErrNoRows {
		_, err = tx.Exec(ctx, `INSERT INTO product_external_ids(product_id,variant_id,provider,id_type,external_id,status,source) VALUES($1,$2,'saby','id',$3,'active','manual')`, productID, variantID, sabyID)
	}
	if err != nil {
		return err
	}
	// Existing local inventory must not become payable merely because the SKU
	// gained an identity. The Saby worker will replace it with an actual count.
	if _, err = tx.Exec(ctx, `UPDATE inventory SET available_qty=reserved_qty,synced_at=TIMESTAMPTZ '1970-01-01 00:00:00+00' WHERE variant_id=$1`, variantID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
