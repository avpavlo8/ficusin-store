package admin

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type ProductRemoval struct {
	ProductID int64  `json:"productId"`
	Action    string `json:"action"`
}

// Product removal is local catalogue cleanup. External cards are never deleted.
// Published cards and any referenced draft remain recoverable in the archive.
func (repository *PostgresRepository) RemoveProduct(ctx context.Context, actor Actor, id int64) (ProductRemoval, error) {
	result := ProductRemoval{ProductID: id}
	if !Can(actor.Role, PermissionProductsManage) {
		return result, ErrForbidden
	}
	if id <= 0 {
		return result, ErrInvalidInput
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	var status string
	if err = tx.QueryRow(ctx, `SELECT status FROM products WHERE id=$1 FOR UPDATE`, id).Scan(&status); errors.Is(err, pgx.ErrNoRows) {
		return result, pgx.ErrNoRows
	} else if err != nil {
		return result, err
	}
	protected := status != "draft"
	if !protected {
		protected, err = productRemovalProtected(ctx, tx, id)
		if err != nil {
			return result, err
		}
	}
	result.Action = "deleted"
	if status != "draft" || protected {
		result.Action = "archived"
		_, err = tx.Exec(ctx, `UPDATE products SET status='archived',updated_at=CURRENT_TIMESTAMP WHERE id=$1`, id)
	} else {
		_, err = tx.Exec(ctx, `DELETE FROM products WHERE id=$1`, id)
	}
	if err != nil {
		return result, err
	}
	if err = insertAudit(ctx, tx, actor, "product."+result.Action, "product", fmt.Sprint(id), map[string]any{"status": status}, result); err != nil {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, err
	}
	return result, nil
}

func productRemovalProtected(ctx context.Context, tx pgx.Tx, id int64) (bool, error) {
	// Locks serialize stock updates and new FK references with the delete decision.
	rows, err := tx.Query(ctx, `SELECT id,NULLIF(BTRIM(saby_id),'') IS NOT NULL FROM product_variants WHERE product_id=$1 ORDER BY id FOR UPDATE`, id)
	if err != nil {
		return false, err
	}
	protected := false
	ids := []int64{}
	for rows.Next() {
		var variant int64
		var linked bool
		if err = rows.Scan(&variant, &linked); err != nil {
			rows.Close()
			return false, err
		}
		ids = append(ids, variant)
		protected = protected || linked
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	if protected {
		return true, nil
	}
	for _, variant := range ids {
		stock, err := tx.Query(ctx, `SELECT available_qty,reserved_qty FROM inventory WHERE variant_id=$1 FOR UPDATE`, variant)
		if err != nil {
			return false, err
		}
		for stock.Next() {
			var available, reserved int
			if err = stock.Scan(&available, &reserved); err != nil {
				stock.Close()
				return false, err
			}
			protected = protected || available != 0 || reserved != 0
		}
		err = stock.Err()
		stock.Close()
		if err != nil {
			return false, err
		}
		if protected {
			return true, nil
		}
		refs, err := catalogueHistoricalReferences(ctx, tx, "product_variants", variant)
		if err != nil {
			return false, err
		}
		if refs {
			return true, nil
		}
	}
	// Marketplace CASCADE links still own remote identity/history. They must
	// force archival even when the generic historical-FK scan permits deletion.
	var external bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM product_external_ids WHERE product_id=$1 AND provider<>'ficusin')
 OR EXISTS(SELECT 1 FROM catalog_external_operation_history e JOIN product_variants v ON v.id=e.variant_id WHERE v.product_id=$1)
 OR EXISTS(SELECT 1 FROM catalog_plan_format_history WHERE product_id=$1)
 OR EXISTS(SELECT 1 FROM product_reviews WHERE product_id=$1)
 OR EXISTS(SELECT 1 FROM avito_listing_products WHERE product_id=$1)
 OR EXISTS(SELECT 1 FROM flowwow_inventory_sync f JOIN product_variants v ON v.id=f.variant_id WHERE v.product_id=$1)
 OR EXISTS(SELECT 1 FROM catalog_editor_link_history h JOIN product_variants v ON v.id=h.variant_id WHERE v.product_id=$1)`, id).Scan(&external); err != nil {
		return false, err
	}
	refs, err := catalogueHistoricalReferences(ctx, tx, "products", id)
	return protected || external || refs, err
}

// Preserve RESTRICT/NO ACTION/SET NULL history even where deletion would merely
// detach the ID. Creation receipts deliberately retain a NULL tombstone after
// deleting a free draft; their immutable key prevents recreation. Other names
// come from PostgreSQL metadata and remain quoted.
func catalogueHistoricalReferences(ctx context.Context, tx pgx.Tx, table string, id int64) (bool, error) {
	rows, err := tx.Query(ctx, `SELECT ns.nspname,c.relname,a.attname FROM pg_constraint fk
 JOIN pg_class c ON c.oid=fk.conrelid JOIN pg_namespace ns ON ns.oid=c.relnamespace
 JOIN pg_attribute a ON a.attrelid=c.oid AND a.attnum=fk.conkey[1]
 WHERE fk.contype='f' AND fk.confrelid=$1::text::regclass AND cardinality(fk.conkey)=1 AND fk.confdeltype IN ('a','r','n')
 AND NOT ($1::text='products' AND ns.nspname=current_schema() AND c.relname='catalog_product_creation_requests')`, table)
	if err != nil {
		return false, err
	}
	type ref struct{ schema, table, column string }
	checks := []ref{}
	for rows.Next() {
		var r ref
		if err = rows.Scan(&r.schema, &r.table, &r.column); err != nil {
			rows.Close()
			return false, err
		}
		checks = append(checks, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	for _, r := range checks {
		var exists bool
		query := fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s WHERE %s=$1)", pgx.Identifier{r.schema, r.table}.Sanitize(), pgx.Identifier{r.column}.Sanitize())
		if err = tx.QueryRow(ctx, query, id).Scan(&exists); err != nil {
			return false, err
		}
		if exists {
			return true, nil
		}
	}
	return false, nil
}
