package catalogeditor

import (
	"context"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"
)

// Only a verified native readback may enter the canonical stock identity. A
// numeric internal ID, business code, UUID and barcode remain distinct values.
func saveLiveSabyLink(ctx context.Context, tx pgx.Tx, product, variant int64, ids RemoteIDs) error {
	id, err := strconv.ParseInt(ids.ProductID, 10, 64)
	if err != nil || id <= 0 || ids.OfferID == "" || ids.UUID == "" {
		return errors.New("СБИС не подтвердил полный набор идентификаторов карточки")
	}
	var current string
	if err = tx.QueryRow(ctx, `SELECT COALESCE(saby_id,'') FROM product_variants WHERE id=$1 AND product_id=$2 FOR UPDATE`, variant, product).Scan(&current); err != nil {
		return err
	}
	if current != "" && current != ids.ProductID {
		return errors.New("Существующая связь СБИС изменилась; прежняя карточка сохранена")
	}
	if _, err = tx.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,article,name) SELECT $1,$2,$3,p.name FROM products p WHERE p.id=$4 ON CONFLICT(saby_id) DO UPDATE SET code=EXCLUDED.code,article=EXCLUDED.article`, ids.ProductID, ids.OfferID, ids.SKU, product); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE product_variants SET saby_id=$2,updated_at=NOW() WHERE id=$1`, variant, ids.ProductID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE products SET saby_id=COALESCE(NULLIF(saby_id,''),$2),saby_fields=CASE WHEN 'stock'=ANY(saby_fields) THEN saby_fields ELSE array_append(saby_fields,'stock') END,updated_at=NOW() WHERE id=$1`, product, ids.ProductID); err != nil {
		return err
	}
	for kind, value := range map[string]string{"id": ids.ProductID, "code": ids.OfferID, "alias": ids.UUID} {
		tag, e := tx.Exec(ctx, `INSERT INTO product_external_ids(product_id,variant_id,provider,id_type,external_id,source,status,is_primary) VALUES($1,$2,'saby',$3,$4,'catalog-editor','active',TRUE) ON CONFLICT(provider,id_type,external_id) DO UPDATE SET variant_id=EXCLUDED.variant_id,status='active',updated_at=NOW() WHERE product_external_ids.product_id=EXCLUDED.product_id AND (product_external_ids.variant_id IS NULL OR product_external_ids.variant_id=EXCLUDED.variant_id)`, product, variant, kind, value)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return errors.New("Идентификатор СБИС уже занят другим вариантом")
		}
	}
	if current == "" {
		// Binding is not stock confirmation. Existing local/supplier quantities
		// stay unavailable until a real, fresh Saby inventory exchange arrives.
		_, err = tx.Exec(ctx, `UPDATE inventory SET available_qty=reserved_qty,synced_at=TIMESTAMPTZ '1970-01-01' WHERE variant_id=$1`, variant)
	}
	return err
}
