package admin

import "context"

type DiscountPlant struct {
	Description     string  `json:"description"`
	ID              int64   `json:"id"`
	Article         string  `json:"article"`
	Name            string  `json:"name"`
	Label           string  `json:"label"`
	Price           float64 `json:"price"`
	DiscountPercent int     `json:"discountPercent"`
	DiscountedPrice float64 `json:"discountedPrice"`
	Image           string  `json:"image"`
}

func (r *PostgresRepository) DiscountPlants(ctx context.Context) ([]DiscountPlant, error) {
	rows, err := r.pool.Query(ctx, `SELECT r.id,'RET-'||CASE WHEN LENGTH(r.id::TEXT)<6 THEN LPAD(r.id::TEXT,6,'0') ELSE r.id::TEXT END,p.name,COALESCE(v.label,''),COALESCE(r.retail_price_minor_snapshot,v.base_price_minor)::DOUBLE PRECISION/100,r.discount_percent,ROUND(COALESCE(r.retail_price_minor_snapshot,v.base_price_minor)::NUMERIC*(100-r.discount_percent)/100)::DOUBLE PRECISION/100,
 CASE WHEN EXISTS(SELECT 1 FROM marketplace_return_photos f WHERE f.marketplace_return_id=r.id AND f.deleted_at IS NULL) THEN '/api/v1/discount-plants/'||r.id||'/photo?v='||(SELECT f.id::TEXT FROM marketplace_return_photos f WHERE f.marketplace_return_id=r.id AND f.deleted_at IS NULL ORDER BY f.id LIMIT 1) ELSE COALESCE((SELECT COALESCE(mirror.card_url,mirror.large_url,m.object_key) FROM product_media m LEFT JOIN media_mirror mirror ON mirror.source_url=m.object_key WHERE m.product_id=p.id AND (m.variant_id=v.id OR m.variant_id IS NULL) ORDER BY m.is_primary DESC,m.sort_order,m.id LIMIT 1),'/assets/hero-monstera.webp') END,r.public_comment
 FROM marketplace_returns r JOIN product_variants v ON v.id=r.canonical_variant_id JOIN products p ON p.id=v.product_id WHERE r.condition='restoring' AND r.discount_percent>0 AND r.deleted_at IS NULL AND COALESCE(r.retail_price_minor_snapshot,v.base_price_minor)>0 ORDER BY r.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []DiscountPlant{}
	for rows.Next() {
		var item DiscountPlant
		if err = rows.Scan(&item.ID, &item.Article, &item.Name, &item.Label, &item.Price, &item.DiscountPercent, &item.DiscountedPrice, &item.Image, &item.Description); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
func (r *PostgresRepository) DiscountPlantPhoto(ctx context.Context, id int64) (string, []byte, error) {
	var mime string
	var image []byte
	err := r.pool.QueryRow(ctx, `SELECT f.content_type,f.image FROM marketplace_return_photos f JOIN marketplace_returns r ON r.id=f.marketplace_return_id WHERE r.id=$1 AND r.condition='restoring' AND r.discount_percent>0 AND r.deleted_at IS NULL AND f.deleted_at IS NULL ORDER BY f.id LIMIT 1`, id).Scan(&mime, &image)
	return mime, image, err
}
