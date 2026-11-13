package admin

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
)

// The variant lock also blocks new FK references until the decision commits.
// Inventory rows are locked separately because quantities can change in place.
func removeProductVariant(ctx context.Context, tx pgx.Tx, actor Actor, productID, variantID int64) error {
	if !Can(actor.Role, PermissionDelete) {
		return ErrForbidden
	}
	var linked bool
	if err := tx.QueryRow(ctx, `SELECT NULLIF(BTRIM(saby_id),'') IS NOT NULL FROM product_variants WHERE id=$1 AND product_id=$2 FOR UPDATE`, variantID, productID).Scan(&linked); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT available_qty,reserved_qty FROM inventory WHERE variant_id=$1 FOR UPDATE`, variantID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var available, reserved int
		if err = rows.Scan(&available, &reserved); err != nil {
			rows.Close()
			return err
		}
		linked = linked || available != 0 || reserved != 0
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var external bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM product_external_ids WHERE variant_id=$1 AND provider='saby') OR EXISTS(SELECT 1 FROM legacy_archive.catalog_saby_exports WHERE variant_id=$1) OR EXISTS(SELECT 1 FROM catalog_editor_price_publications WHERE variant_id=$1)`, variantID).Scan(&external); err != nil {
		return err
	}
	linked = linked || external
	// Preserve order, procurement, return and other historical references, even
	// where an old schema would have silently replaced the variant ID with NULL.
	refs, err := tx.Query(ctx, `SELECT ns.nspname,c.relname,a.attname FROM pg_constraint fk JOIN pg_class c ON c.oid=fk.conrelid JOIN pg_namespace ns ON ns.oid=c.relnamespace JOIN pg_attribute a ON a.attrelid=c.oid AND a.attnum=fk.conkey[1] WHERE fk.contype='f' AND fk.confrelid='product_variants'::regclass AND cardinality(fk.conkey)=1 AND fk.confdeltype IN ('a','r','n')`)
	if err != nil {
		return err
	}
	type reference struct{ schema, table, column string }
	checks := []reference{}
	for refs.Next() {
		var r reference
		if err = refs.Scan(&r.schema, &r.table, &r.column); err != nil {
			refs.Close()
			return err
		}
		checks = append(checks, r)
	}
	err = refs.Err()
	refs.Close()
	if err != nil {
		return err
	}
	for _, r := range checks {
		var exists bool
		query := fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s WHERE %s=$1)", pgx.Identifier{r.schema, r.table}.Sanitize(), pgx.Identifier{r.column}.Sanitize())
		if err = tx.QueryRow(ctx, query, variantID).Scan(&exists); err != nil {
			return err
		}
		linked = linked || exists
	}
	action := "variant.delete"
	if linked {
		action = "variant.archive"
		_, err = tx.Exec(ctx, `UPDATE product_variants SET is_active=0,archived_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, variantID)
	} else {
		// The shared product cover survives deletion of its former size.
		if _, err = tx.Exec(ctx, `UPDATE product_media SET variant_id=NULL WHERE variant_id=$1 AND is_primary=1`, variantID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM product_variants WHERE id=$1`, variantID)
	}
	if err != nil {
		return err
	}
	return insertAudit(ctx, tx, actor, action, "variant", fmt.Sprint(variantID), map[string]any{"productId": productID}, map[string]any{"archived": linked})
}
