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
	LinkedProductID *int64 `json:"linkedProductId,omitempty"`
	MappedProductID *int64 `json:"mappedProductId,omitempty"`
}

// SearchSabyLinkCandidates reads the last imported Saby directory. The search
// does not contact Saby or change catalogue data.
func (repository *PostgresRepository) SearchSabyLinkCandidates(ctx context.Context, query string) ([]SabyLinkCandidate, error) {
	query = strings.TrimSpace(query)
	if len([]rune(query)) < 2 || len([]rune(query)) > 100 {
		return nil, fmt.Errorf("%w: введите от 2 до 100 символов", ErrInvalidInput)
	}
	rows, err := repository.pool.Query(ctx, `SELECT n.saby_id,n.code,n.article,n.name,n.missing_since IS NOT NULL,v.id,p.id,e.product_id
		FROM saby_nomenclature n LEFT JOIN product_variants v ON v.saby_id=n.saby_id
		LEFT JOIN products p ON p.saby_id=n.saby_id
		LEFT JOIN product_external_ids e ON e.provider='saby' AND e.id_type='id' AND e.external_id=n.saby_id
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
		if err := rows.Scan(&item.ID, &item.Code, &item.Article, &item.Name, &item.Missing, &item.LinkedVariantID, &item.LinkedProductID, &item.MappedProductID); err != nil {
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
	var sku string
	var archived bool
	if err = tx.QueryRow(ctx, `SELECT product_id,sku,COALESCE(saby_id,''),archived_at IS NOT NULL FROM product_variants WHERE id=$1 FOR UPDATE`, variantID).Scan(&productID, &sku, &currentID, &archived); err != nil {
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
	if _, err = tx.Exec(ctx, `UPDATE products SET saby_id=COALESCE(NULLIF(saby_id,''),$2),saby_fields=CASE WHEN 'stock'=ANY(saby_fields) THEN saby_fields ELSE array_append(saby_fields,'stock') END,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, productID, sabyID); err != nil {
		return err
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
	if err = insertAudit(ctx, tx, actor, "variant.saby.link", "variant", fmt.Sprint(variantID), map[string]any{"sku": sku, "sabyId": currentID}, map[string]any{"sku": sku, "productId": productID, "sabyId": sabyID}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RebindVariantToSaby replaces a vanished Saby stock identity while retaining
// the site's product, SKU, content and historical external identifiers.
// Stock stays unavailable until the next confirmed Saby snapshot.
func (repository *PostgresRepository) RebindVariantToSaby(ctx context.Context, actor Actor, variantID int64, oldID, newID string) error {
	if actor.Role != RoleOwner {
		return ErrForbidden
	}
	oldID, newID = strings.TrimSpace(oldID), strings.TrimSpace(newID)
	if variantID < 1 || oldID == "" || newID == "" || oldID == newID {
		return fmt.Errorf("%w: укажите вариант, старую и новую записи СБИС", ErrInvalidInput)
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var productID int64
	var sku, currentID string
	var archived bool
	if err = tx.QueryRow(ctx, `SELECT product_id,sku,COALESCE(saby_id,''),archived_at IS NOT NULL FROM product_variants WHERE id=$1 FOR UPDATE`, variantID).Scan(&productID, &sku, &currentID, &archived); err != nil {
		return err
	}
	if archived || currentID != oldID {
		return fmt.Errorf("%w: текущая связь варианта изменилась", ErrInvalidInput)
	}
	var oldStillCurrent, otherUsesOld bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM saby_nomenclature WHERE saby_id=$1 AND missing_since IS NULL),
		EXISTS(SELECT 1 FROM product_variants WHERE saby_id=$1 AND id<>$2)`, oldID, variantID).Scan(&oldStillCurrent, &otherUsesOld); err != nil {
		return err
	}
	if oldStillCurrent || otherUsesOld {
		return fmt.Errorf("%w: старая запись ещё актуальна или используется другим размером", ErrInvalidInput)
	}
	var newCode string
	var missing bool
	if err = tx.QueryRow(ctx, `SELECT COALESCE(code,''),missing_since IS NOT NULL FROM saby_nomenclature WHERE saby_id=$1 FOR UPDATE`, newID).Scan(&newCode, &missing); err != nil {
		return err
	}
	if missing {
		return fmt.Errorf("%w: новая запись отсутствует в последней выгрузке СБИС", ErrInvalidInput)
	}
	var occupied, reserved bool
	if err = tx.QueryRow(ctx, `SELECT
		EXISTS(SELECT 1 FROM products WHERE saby_id=$1 AND id<>$2)
		OR EXISTS(SELECT 1 FROM product_variants WHERE saby_id=$1 AND id<>$3)
		OR EXISTS(SELECT 1 FROM product_external_ids WHERE provider='saby' AND id_type='id' AND external_id=$1 AND (product_id<>$2 OR variant_id IS NOT NULL AND variant_id<>$3)),
		EXISTS(SELECT 1 FROM inventory WHERE variant_id=$3 AND reserved_qty>0)`, newID, productID, variantID).Scan(&occupied, &reserved); err != nil {
		return err
	}
	if occupied || reserved {
		return fmt.Errorf("%w: новая запись уже связана или по варианту есть резерв", ErrInvalidInput)
	}
	if newCode != "" {
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM product_external_ids WHERE provider='saby' AND id_type='code' AND external_id=$1 AND (product_id<>$2 OR variant_id IS NOT NULL AND variant_id<>$3))`, newCode, productID, variantID).Scan(&occupied); err != nil {
			return err
		}
		if occupied {
			return fmt.Errorf("%w: новый X-код уже связан с другой карточкой", ErrInvalidInput)
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE product_external_ids SET status='legacy',is_primary=FALSE,updated_at=CURRENT_TIMESTAMP
		WHERE product_id=$1 AND provider='saby' AND id_type IN ('id','code') AND
		(variant_id=$2 OR variant_id IS NULL AND id_type='id' AND external_id=$3)`, productID, variantID, oldID); err != nil {
		return err
	}
	// A single-size legacy card may also have its old X-code on the product.
	if _, err = tx.Exec(ctx, `UPDATE product_external_ids SET status='legacy',is_primary=FALSE,updated_at=CURRENT_TIMESTAMP
		WHERE product_id=$1 AND variant_id IS NULL AND provider='saby' AND id_type='code'
		AND (SELECT COUNT(*) FROM product_variants WHERE product_id=$1 AND archived_at IS NULL)=1`, productID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE product_variants SET saby_id=$2,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, variantID, newID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE products SET saby_id=CASE WHEN saby_id=$2 THEN $3 ELSE saby_id END,
		updated_at=CURRENT_TIMESTAMP WHERE id=$1`, productID, oldID, newID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO product_external_ids(product_id,variant_id,provider,id_type,external_id,status,is_primary,source,linked_by,confirmed_at)
		VALUES($1,$2,'saby','id',$3,'active',TRUE,'manual',$4,CURRENT_TIMESTAMP)
		ON CONFLICT(provider,id_type,external_id) DO UPDATE SET variant_id=EXCLUDED.variant_id,status='active',is_primary=TRUE,
			source='manual',linked_by=EXCLUDED.linked_by,confirmed_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP`, productID, variantID, newID, actor.CustomerID); err != nil {
		return err
	}
	if newCode != "" {
		if _, err = tx.Exec(ctx, `INSERT INTO product_external_ids(product_id,variant_id,provider,id_type,external_id,status,is_primary,source,linked_by,confirmed_at)
			VALUES($1,$2,'saby','code',$3,'active',TRUE,'manual',$4,CURRENT_TIMESTAMP)
			ON CONFLICT(provider,id_type,external_id) DO UPDATE SET variant_id=EXCLUDED.variant_id,status='active',is_primary=TRUE,
				source='manual',linked_by=EXCLUDED.linked_by,confirmed_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP`, productID, variantID, newCode, actor.CustomerID); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE inventory SET available_qty=0,synced_at=TIMESTAMPTZ '1970-01-01 00:00:00+00' WHERE variant_id=$1`, variantID); err != nil {
		return err
	}
	if err = insertAudit(ctx, tx, actor, "variant.saby.relink", "variant", fmt.Sprint(variantID),
		map[string]any{"sku": sku, "sabyId": oldID}, map[string]any{"sku": sku, "productId": productID, "sabyId": newID, "code": newCode}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
