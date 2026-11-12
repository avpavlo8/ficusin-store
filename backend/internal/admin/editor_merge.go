package admin

import (
	"context"
	"github.com/jackc/pgx/v5"
)

// Plans belong to immutable variants. Combining their parent product must not
// discard the source variants' channel categories, attributes or identities.
func moveMergedEditorPlan(ctx context.Context, tx pgx.Tx, target, source int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO catalog_editor_plans(product_id,config)
  SELECT $1,jsonb_build_object('variants',COALESCE(config->'variants','[]'::jsonb)) FROM catalog_editor_plans WHERE product_id=$2
  ON CONFLICT(product_id) DO UPDATE SET config=jsonb_set(catalog_editor_plans.config,'{variants}',COALESCE(catalog_editor_plans.config->'variants','[]'::jsonb)||(EXCLUDED.config->'variants')),updated_at=NOW()`, target, source)
	return err
}
