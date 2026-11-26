package procurement

import (
	"context"
	"fmt"
)

func (store *PostgresStore) SearchSiteNomenclature(ctx context.Context, query string) ([]NomenclatureCandidate, error) {
	rows, err := store.pool.Query(ctx, `SELECT d.variant_id, COALESCE(n.saby_id,''),
 d.display_sku, '', CONCAT_WS(' · ',d.name,NULLIF(d.label,'')),
 COALESCE(n.balance,0),v.base_price_minor::DOUBLE PRECISION/100
 FROM canonical_product_directory d JOIN product_variants v ON v.id=d.variant_id
 LEFT JOIN saby_nomenclature n ON n.saby_id=d.saby_id AND n.missing_since IS NULL
 WHERE d.active AND d.product_status<>'archived' AND
  (CONCAT_WS(' ',d.name,d.label) ILIKE '%'||$1||'%' OR d.display_sku ILIKE '%'||$1||'%')
 ORDER BY (LOWER(d.display_sku)=LOWER($1)) DESC, (d.product_status='published') DESC, d.name,d.variant_id LIMIT 30`, query)
	if err != nil {
		return nil, fmt.Errorf("search procurement site catalogue: %w", err)
	}
	defer rows.Close()
	items := make([]NomenclatureCandidate, 0, 30)
	for rows.Next() {
		var item NomenclatureCandidate
		if err := rows.Scan(&item.VariantID, &item.SabyID, &item.Code, &item.Article, &item.Name, &item.Balance, &item.Price); err != nil {
			return nil, fmt.Errorf("scan procurement site catalogue: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
