package admin

import "context"

// Every order response uses the same media projection. An update response must
// not erase covers that were present in ListOrders when replacing frontend state.
func (repository *PostgresRepository) hydrateOrderItemMedia(ctx context.Context, items []*OrderItem) error {
	if len(items) == 0 {
		return nil
	}
	products := make([]int64, len(items))
	skus := make([]string, len(items))
	for i, item := range items {
		products[i] = item.ProductID
		skus[i] = item.SKU
	}
	rows, err := repository.pool.Query(ctx, `
 SELECT requested.position,COALESCE(media.image,'/assets/product-photo-pending-graphite-v1.svg'),COALESCE(media.fallback,'')
 FROM unnest($1::bigint[],$2::text[]) WITH ORDINALITY requested(product_id,sku,position)
 LEFT JOIN LATERAL (
  SELECT COALESCE(NULLIF(m.card_url,''),NULLIF(m.large_url,''),p.object_key) image,
   CASE WHEN NULLIF(m.card_url,'') IS NOT NULL THEN COALESCE(NULLIF(m.large_url,''),p.object_key) ELSE p.object_key END fallback
  FROM product_media p LEFT JOIN media_mirror m ON m.source_url=p.object_key
  WHERE p.product_id=requested.product_id
   AND (p.variant_id IS NULL OR p.variant_id=(SELECT id FROM product_variants WHERE sku=requested.sku))
   AND p.object_key !~* '\.(mp4|mov|webm)(\?|$)'
   AND NOT EXISTS(SELECT 1 FROM catalog_supplementary_media s WHERE s.product_id=p.product_id AND s.object_key=p.object_key)
  ORDER BY p.is_primary DESC,(p.variant_id IS NULL) DESC,p.sort_order,p.id LIMIT 1
 ) media ON TRUE ORDER BY requested.position`, products, skus)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var position int
		var image, fallback string
		if err = rows.Scan(&position, &image, &fallback); err != nil {
			return err
		}
		items[position-1].Image = image
		if fallback != image {
			items[position-1].ImageFallback = fallback
		}
	}
	return rows.Err()
}
