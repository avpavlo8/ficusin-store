package admin

// Shared by the list and detail so variant photos and fallback stay consistent.
const orderItemImageSQL = `COALESCE((
				SELECT COALESCE(mirror.card_url, mirror.large_url, media.object_key)
				FROM product_media media
				LEFT JOIN media_mirror mirror ON mirror.source_url = media.object_key
				WHERE media.product_id = item.product_id
					AND (media.variant_id = item.variant_id OR media.variant_id IS NULL)
					AND NOT EXISTS (SELECT 1 FROM catalog_supplementary_media supplementary
						WHERE supplementary.product_id = media.product_id AND supplementary.object_key = media.object_key)
				ORDER BY (media.variant_id = item.variant_id) DESC NULLS LAST, media.is_primary DESC, media.sort_order, media.id
				LIMIT 1
			), '/assets/product-photo-pending-graphite-v1.svg')`
