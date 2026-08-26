package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

// DetailBySlug keeps the old method name only to avoid widening the repository
// interface during this release. The argument is now the numeric Ficusin
// product code; old name/Saby slugs are removed by migration 055.
func (repository *PostgresRepository) DetailBySlug(ctx context.Context, code string) (ProductDetail, error) {
	resolvedCode, err := repository.resolveLegacyCode(ctx, code)
	if err != nil {
		return ProductDetail{}, err
	}
	code = resolvedCode
	var detail ProductDetail
	var productID int64
	err = repository.pool.QueryRow(ctx, `
		SELECT id, product_code::TEXT, name, latin_name, short_description, description, care_instructions,
			catalog_section, COALESCE(plant_kind, ''), COALESCE(light_level, ''),
			COALESCE(watering, ''), COALESCE(height_class, ''), COALESCE(care_level, ''),
			COALESCE(placement, ''), COALESCE(pet_safety, ''), COALESCE(growth_habit, ''), category_id,
			plant_passport, important_warnings,
			COALESCE((SELECT AVG(rating)::float8 FROM product_reviews WHERE product_id = products.id AND status = 'published'), 0),
			(SELECT COUNT(*) FROM product_reviews WHERE product_id = products.id AND status = 'published')
		FROM products WHERE product_code::TEXT = $1 AND status = 'published' LIMIT 1
	`, code).Scan(&productID, &detail.ID, &detail.Name, &detail.Latin,
		&detail.ShortDescription, &detail.Description, &detail.CareInstructions,
		&detail.CatalogSection, &detail.PlantKind, &detail.LightLevel,
		&detail.Watering, &detail.HeightClass, &detail.CareLevel,
		&detail.Placement, &detail.PetSafety, &detail.GrowthHabit, &detail.CategoryID,
		&detail.Passport, &detail.ImportantWarnings, &detail.Rating, &detail.ReviewsCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProductDetail{}, ErrNotFound
	}
	if err != nil {
		return ProductDetail{}, fmt.Errorf("query product detail: %w", err)
	}

	mediaRows, err := repository.pool.Query(ctx, `
		SELECT COALESCE(mirror.large_url, media.object_key)
		FROM product_media media
		LEFT JOIN media_mirror mirror ON mirror.source_url = media.object_key
		WHERE media.product_id = $1 AND media.variant_id IS NULL
		ORDER BY media.is_primary DESC, media.sort_order, media.id
	`, productID)
	if err != nil {
		return ProductDetail{}, fmt.Errorf("query product media: %w", err)
	}
	detail.Images = []string{}
	for mediaRows.Next() {
		var image string
		if err := mediaRows.Scan(&image); err != nil {
			mediaRows.Close()
			return ProductDetail{}, err
		}
		detail.Images = append(detail.Images, image)
	}
	if err := mediaRows.Err(); err != nil {
		mediaRows.Close()
		return ProductDetail{}, fmt.Errorf("read product media: %w", err)
	}
	mediaRows.Close()
	// During the transition existing photos can still be attached without a
	// variant marker. If the common gallery is empty, include all product media.
	if len(detail.Images) == 0 {
		rows, mediaErr := repository.pool.Query(ctx, `
			SELECT COALESCE(mirror.large_url, media.object_key)
			FROM product_media media
			LEFT JOIN media_mirror mirror ON mirror.source_url = media.object_key
			WHERE media.product_id = $1
			ORDER BY media.is_primary DESC, media.sort_order, media.id
		`, productID)
		if mediaErr == nil {
			for rows.Next() {
				var image string
				if rows.Scan(&image) == nil {
					detail.Images = append(detail.Images, image)
				}
			}
			rows.Close()
		}
	}
	if len(detail.Images) == 0 {
		detail.Images = append(detail.Images, "/assets/hero-monstera.webp")
	}

	variantRows, err := repository.pool.Query(ctx, `
		SELECT pv.id, pv.sku, pv.label, pv.base_price_minor,
			COALESCE((SELECT SUM(GREATEST(i.available_qty-i.reserved_qty,0)) FROM inventory i WHERE i.variant_id=pv.id),0)::INTEGER,
			variant_numeric_attribute(pv.id, 'height_cm')::INTEGER,
			variant_numeric_attribute(pv.id, 'pot_diameter_cm')::INTEGER, pv.wholesale_min_qty,
			COALESCE((SELECT jsonb_agg(COALESCE(mirror.large_url,media.object_key) ORDER BY media.is_primary DESC,media.sort_order,media.id)
				FROM product_media media LEFT JOIN media_mirror mirror ON mirror.source_url=media.object_key
				WHERE media.variant_id=pv.id),'[]'::jsonb),
			COALESCE((
				WITH RECURSIVE ancestors AS (
					SELECT id,parent_id,0 depth FROM categories WHERE id=$2
					UNION ALL SELECT c.id,c.parent_id,a.depth+1 FROM categories c JOIN ancestors a ON a.parent_id=c.id
				), effective AS (
					SELECT DISTINCT ON (d.id) d.id,d.code,d.name,d.unit,d.audience,d.value_scope,
						ca.is_badge,ca.is_filterable,ca.show_in_summary,ca.summary_position,
						ca.show_in_characteristics,ca.is_excluded,ca.sort_order,a.depth
					FROM ancestors a JOIN category_attributes ca ON ca.category_id=a.id
					JOIN attribute_definitions d ON d.id=ca.attribute_id AND d.is_active
					ORDER BY d.id,a.depth
				)
				SELECT jsonb_agg(jsonb_build_object(
					'code',e.code,'name',e.name,'unit',e.unit,'value',v.value,
					'badge',e.is_badge,'filterable',e.is_filterable,
					'summaryPosition',e.summary_position,
					'showInCharacteristics',e.show_in_characteristics
				) ORDER BY e.sort_order,e.code)
				FROM effective e
				JOIN variant_attribute_values v ON v.attribute_id=e.id AND v.variant_id=pv.id
				WHERE e.audience='customer' AND e.value_scope='variant' AND NOT e.is_excluded
				  AND (e.show_in_summary OR e.show_in_characteristics OR e.is_badge OR e.is_filterable)
			), '[]'::jsonb)
		FROM product_variants pv
		WHERE pv.product_id = $1 AND pv.is_active = 1 AND pv.archived_at IS NULL
		ORDER BY pv.id
	`, productID, detail.CategoryID)
	if err != nil {
		return ProductDetail{}, fmt.Errorf("query product variants: %w", err)
	}
	detail.Variants = []Variant{}
	for variantRows.Next() {
		var variant Variant
		var priceMinor int64
		var attributes, images []byte
		if err := variantRows.Scan(&variant.ID, &variant.SKU, &variant.Label, &priceMinor,
			&variant.Stock, &variant.HeightCM, &variant.PotDiameterCM,
			&variant.WholesaleMinQty, &images, &attributes); err != nil {
			variantRows.Close()
			return ProductDetail{}, err
		}
		variant.Price = float64(priceMinor) / 100
		if err := json.Unmarshal(images, &variant.Images); err != nil {
			variantRows.Close()
			return ProductDetail{}, fmt.Errorf("decode variant images: %w", err)
		}
		if err := json.Unmarshal(attributes, &variant.Attributes); err != nil {
			variantRows.Close()
			return ProductDetail{}, fmt.Errorf("decode variant attributes: %w", err)
		}
		detail.Variants = append(detail.Variants, variant)
	}
	if err := variantRows.Err(); err != nil {
		variantRows.Close()
		return ProductDetail{}, fmt.Errorf("read product variants: %w", err)
	}
	variantRows.Close()

	attributeRows, err := repository.pool.Query(ctx, `
		WITH RECURSIVE ancestors AS (
			SELECT id,parent_id,0 depth FROM categories WHERE id=$2
			UNION ALL SELECT c.id,c.parent_id,a.depth+1 FROM categories c JOIN ancestors a ON a.parent_id=c.id
		), effective AS (
			SELECT DISTINCT ON (d.id) d.id,d.code,d.name,d.unit,d.audience,d.value_scope,
				ca.is_badge,ca.is_filterable,ca.show_in_summary,ca.summary_position,
				ca.show_in_characteristics,ca.is_excluded,ca.sort_order,a.depth
			FROM ancestors a JOIN category_attributes ca ON ca.category_id=a.id
			JOIN attribute_definitions d ON d.id=ca.attribute_id AND d.is_active
			ORDER BY d.id,a.depth
		)
		SELECT e.code,e.name,e.unit,v.value,e.is_badge,e.is_filterable,
			e.summary_position,e.show_in_characteristics
		FROM effective e
		JOIN product_attribute_values v ON v.attribute_id=e.id AND v.product_id=$1
		WHERE e.audience='customer' AND e.value_scope='product' AND NOT e.is_excluded
		  AND (e.show_in_summary OR e.show_in_characteristics OR e.is_badge OR e.is_filterable)
		ORDER BY e.sort_order,e.code
	`, productID, detail.CategoryID)
	if err != nil {
		return ProductDetail{}, fmt.Errorf("query product attributes: %w", err)
	}
	detail.Attributes = []ProductAttribute{}
	for attributeRows.Next() {
		var item ProductAttribute
		if err := attributeRows.Scan(&item.Code, &item.Name, &item.Unit, &item.Value, &item.Badge,
			&item.Filterable, &item.SummaryPosition, &item.ShowInCharacteristics); err != nil {
			attributeRows.Close()
			return ProductDetail{}, err
		}
		detail.Attributes = append(detail.Attributes, item)
	}
	if err := attributeRows.Err(); err != nil {
		attributeRows.Close()
		return ProductDetail{}, fmt.Errorf("read product attributes: %w", err)
	}
	attributeRows.Close()

	reviewRows, err := repository.pool.Query(ctx, `
		SELECT r.id, r.rating, r.body, COALESCE(NULLIF(c.full_name, ''), 'Покупатель'),
			to_char(r.created_at, 'YYYY-MM-DD'), true,
			COALESCE(jsonb_agg(jsonb_build_object(
				'url','/api/v1/review-photos/' || photo.id,
				'contentType',photo.content_type
			) ORDER BY photo.sort_order,photo.id) FILTER (WHERE photo.id IS NOT NULL),'[]'::jsonb)
		FROM product_reviews r
		JOIN customers c ON c.id = r.customer_id
		LEFT JOIN product_review_photos photo ON photo.review_id=r.id
		WHERE r.product_id = $1 AND r.status = 'published'
		GROUP BY r.id,c.id ORDER BY r.created_at DESC LIMIT 30`, productID)
	if err != nil {
		return ProductDetail{}, fmt.Errorf("query reviews: %w", err)
	}
	detail.Reviews = []Review{}
	for reviewRows.Next() {
		var review Review
		var media []byte
		if err := reviewRows.Scan(&review.ID, &review.Rating, &review.Text, &review.Author, &review.Date, &review.VerifiedPurchase, &media); err != nil {
			reviewRows.Close()
			return ProductDetail{}, err
		}
		if err := json.Unmarshal(media, &review.Media); err != nil {
			reviewRows.Close()
			return ProductDetail{}, fmt.Errorf("decode review media: %w", err)
		}
		for _, item := range review.Media {
			if strings.HasPrefix(item.ContentType, "image/") {
				review.Photos = append(review.Photos, item.URL)
			}
		}
		detail.Reviews = append(detail.Reviews, review)
	}
	reviewRows.Close()

	detail.Recommendations, err = repository.listRecommendations(ctx, productID, detail)
	if err != nil { return ProductDetail{}, err }
	return detail, nil
}

// resolveLegacyCode preserves indexed links from the catalogue before the
// immutable numeric Ficusin codes were introduced. An alias is accepted only
// when the surviving Saby identifier points to exactly one published product;
// ambiguity deliberately remains a 404 instead of redirecting to a wrong item.
func (repository *PostgresRepository) resolveLegacyCode(ctx context.Context, code string) (string, error) {
	const prefix = "saby-"
	if !strings.HasPrefix(strings.ToLower(code), prefix) {
		return code, nil
	}
	var resolved string
	err := repository.pool.QueryRow(ctx, `
		SELECT product.product_code::TEXT
		FROM product_url_aliases alias
		JOIN products product ON product.id=alias.product_id
		WHERE alias.alias=LOWER(BTRIM($1))
	`, code).Scan(&resolved)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("resolve product URL alias: %w", err)
	}
	externalID := strings.TrimSpace(code[len(prefix):])
	if externalID == "" {
		return code, nil
	}
	err = repository.pool.QueryRow(ctx, `
		SELECT MIN(product_code)
		FROM (
			SELECT DISTINCT product.product_code::TEXT AS product_code
			FROM product_external_ids external
			JOIN products product ON product.id=external.product_id
			WHERE external.provider='saby'
				AND LOWER(BTRIM(external.external_id))=LOWER($1)
				AND product.status='published'
		) candidates
		HAVING COUNT(*)=1
	`, externalID).Scan(&resolved)
	if errors.Is(err, pgx.ErrNoRows) {
		return code, nil
	}
	if err != nil {
		return "", fmt.Errorf("resolve legacy product code: %w", err)
	}
	return resolved, nil
}

const recommendationsQuery = `
	SELECT product.product_code::TEXT, chosen.sku, product.name, product.latin_name,
		COALESCE(category.name,'Без категории'), chosen.base_price_minor,
		COALESCE((
			SELECT COALESCE(mirror.card_url,media.object_key)
			FROM product_media media LEFT JOIN media_mirror mirror ON mirror.source_url=media.object_key
			WHERE media.product_id=product.id AND (media.variant_id IS NULL OR media.variant_id=chosen.id)
			ORDER BY (media.variant_id=chosen.id) DESC,media.is_primary DESC,media.sort_order,media.id LIMIT 1
		),'/assets/hero-monstera.webp'),
		chosen.label, chosen.stock, product.catalog_section,
		COALESCE(product.plant_kind,''),COALESCE(product.light_level,''),COALESCE(product.watering,''),
		COALESCE(product.height_class,''),COALESCE(product.care_level,''),COALESCE(product.placement,''),
		COALESCE(product.pet_safety,''),COALESCE(product.growth_habit,''),product.category_id
	FROM products product
	JOIN LATERAL (
		SELECT variant.id,variant.sku,variant.label,variant.base_price_minor,
			COALESCE((SELECT SUM(GREATEST(inventory.available_qty-inventory.reserved_qty,0))
				FROM inventory WHERE inventory.variant_id=variant.id),0)::INTEGER AS stock
		FROM product_variants variant
		WHERE variant.product_id=product.id AND variant.is_active=1 AND variant.archived_at IS NULL
		ORDER BY (COALESCE((SELECT SUM(GREATEST(inventory.available_qty-inventory.reserved_qty,0))
			FROM inventory WHERE inventory.variant_id=variant.id),0)>0) DESC,variant.id LIMIT 1
	) chosen ON TRUE
	LEFT JOIN categories category ON category.id=product.category_id
	WHERE product.status='published' AND product.id<>$1
	ORDER BY (
		CASE WHEN product.category_id=$2 THEN 8 ELSE 0 END +
		CASE WHEN product.catalog_section=$3 THEN 6 ELSE 0 END +
		CASE WHEN $4<>'' AND product.plant_kind=$4 THEN 4 ELSE 0 END +
		CASE WHEN $5<>'' AND product.light_level=$5 THEN 2 ELSE 0 END +
		CASE WHEN $6<>'' AND product.watering=$6 THEN 2 ELSE 0 END +
		CASE WHEN $7<>'' AND product.height_class=$7 THEN 2 ELSE 0 END +
		CASE WHEN $8<>'' AND product.care_level=$8 THEN 2 ELSE 0 END +
		CASE WHEN $9<>'' AND product.placement=$9 THEN 2 ELSE 0 END +
		CASE WHEN $10<>'' AND product.pet_safety=$10 THEN 2 ELSE 0 END +
		CASE WHEN $11<>'' AND product.growth_habit=$11 THEN 2 ELSE 0 END
	) DESC, chosen.stock>0 DESC, product.is_featured DESC, product.name
	LIMIT 8
`

func (repository *PostgresRepository) listRecommendations(ctx context.Context, productID int64, detail ProductDetail) ([]Product, error) {
	rows, err := repository.pool.Query(ctx, recommendationsQuery, productID, detail.CategoryID,
		detail.CatalogSection, detail.PlantKind, detail.LightLevel, detail.Watering,
		detail.HeightClass, detail.CareLevel, detail.Placement, detail.PetSafety, detail.GrowthHabit)
	if err != nil { return nil, fmt.Errorf("query product recommendations: %w", err) }
	defer rows.Close()
	result := make([]Product, 0, 8)
	for rows.Next() {
		var product Product
		var priceMinor int64
		if err := rows.Scan(&product.ID,&product.SKU,&product.Name,&product.Latin,&product.Category,&priceMinor,
			&product.Image,&product.Size,&product.Stock,&product.CatalogSection,&product.PlantKind,&product.LightLevel,
			&product.Watering,&product.HeightClass,&product.CareLevel,&product.Placement,&product.PetSafety,&product.GrowthHabit,
			&product.CategoryID); err != nil { return nil, fmt.Errorf("scan product recommendation: %w", err) }
		product.Price = float64(priceMinor)/100
		product.Light = "Уточните у консультанта"
		product.Collections = []string{}
		product.FilterAttributes = []ProductAttribute{}
		result = append(result, product)
	}
	if err := rows.Err(); err != nil { return nil, fmt.Errorf("read product recommendations: %w", err) }
	return result, nil
}

// recommendationScore mirrors the SQL weights and keeps their ordering easy
// to regression-test without a database.
func recommendationScore(current ProductDetail, candidate Product) int {
	score := 0
	if current.CatalogSection == candidate.CatalogSection {
		score += 6
	}
	if current.CategoryID != nil && candidate.CategoryID != nil && *current.CategoryID == *candidate.CategoryID {
		score += 8
	}
	pairs := [][2]string{
		{current.PlantKind, candidate.PlantKind}, {current.LightLevel, candidate.LightLevel},
		{current.Watering, candidate.Watering}, {current.HeightClass, candidate.HeightClass},
		{current.CareLevel, candidate.CareLevel}, {current.Placement, candidate.Placement},
		{current.PetSafety, candidate.PetSafety}, {current.GrowthHabit, candidate.GrowthHabit},
	}
	for index, pair := range pairs {
		if pair[0] != "" && pair[0] == pair[1] {
			if index == 0 {
				score += 4
			} else {
				score += 2
			}
		}
	}
	return score
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (repository *PostgresRepository) ListCategories(ctx context.Context) ([]Category, error) {
	rows, err := repository.pool.Query(ctx, `SELECT id,parent_id,name,slug,sort_order,icon FROM categories WHERE active=1 ORDER BY sort_order,name`)
	if err != nil {
		return nil, fmt.Errorf("query categories: %w", err)
	}
	defer rows.Close()
	result := make([]Category, 0)
	for rows.Next() {
		var item Category
		if err := rows.Scan(&item.ID, &item.ParentID, &item.Name, &item.Slug, &item.SortOrder, &item.Icon); err != nil {
			return nil, fmt.Errorf("scan category: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

const catalogListQuery = `
	WITH popularity AS (
		SELECT variant.product_id, SUM(oi.quantity * CASE
			WHEN o.created_at >= CURRENT_TIMESTAMP - INTERVAL '30 days' THEN 1.0
			WHEN o.created_at >= CURRENT_TIMESTAMP - INTERVAL '90 days' THEN 0.5
			WHEN o.created_at >= CURRENT_TIMESTAMP - INTERVAL '365 days' THEN 0.2
			ELSE 0.05 END)::DOUBLE PRECISION AS score
		FROM order_items oi
		JOIN product_variants variant ON variant.id=oi.variant_id
		JOIN orders o ON o.id=oi.order_id
		WHERE o.status <> 'cancelled' AND (o.status = 'completed' OR o.payment_status = 'paid')
		GROUP BY variant.product_id
	), default_variants AS (
		SELECT product.id AS product_id, chosen.id, chosen.sku, chosen.label, chosen.base_price_minor, chosen.stock
		FROM products product
		JOIN LATERAL (
			SELECT variant.id,variant.sku,variant.label,variant.base_price_minor,
				COALESCE((SELECT SUM(GREATEST(inventory.available_qty-inventory.reserved_qty,0)) FROM inventory WHERE inventory.variant_id=variant.id),0)::INTEGER AS stock
			FROM product_variants variant
			WHERE variant.product_id=product.id AND variant.is_active=1 AND variant.archived_at IS NULL
			ORDER BY (COALESCE((SELECT SUM(GREATEST(inventory.available_qty-inventory.reserved_qty,0)) FROM inventory WHERE inventory.variant_id=variant.id),0)>0) DESC, variant.id
			LIMIT 1
		) chosen ON TRUE
	)
	SELECT
		product.product_code::TEXT, default_variant.sku, product.name, product.latin_name, COALESCE(root_category.name,'Без категории'),
		default_variant.base_price_minor,
		COALESCE((
			SELECT COALESCE(mirror.card_url,media.object_key)
			FROM product_media media LEFT JOIN media_mirror mirror ON mirror.source_url=media.object_key
			WHERE media.product_id=product.id AND (media.variant_id IS NULL OR media.variant_id=default_variant.id)
			ORDER BY (media.variant_id=default_variant.id) DESC,media.is_primary DESC,media.sort_order,media.id LIMIT 1
		),'/assets/hero-monstera.webp'),
		default_variant.label, default_variant.stock,
		product.catalog_section,COALESCE(product.plant_kind,''),COALESCE(product.light_level,''),
		COALESCE(product.watering,''),COALESCE(product.height_class,''),COALESCE(product.care_level,''),
		COALESCE(product.placement,''),COALESCE(product.pet_safety,''),COALESCE(product.growth_habit,''),product.category_id,
		COALESCE(popularity.score,0),
		COALESCE((SELECT ARRAY_AGG(collection.slug ORDER BY collection.sort_order,collection.id)
			FROM collections collection
			WHERE collection.is_active=1 AND ((collection.mode='manual' AND EXISTS(SELECT 1 FROM collection_products member WHERE member.collection_id=collection.id AND member.product_id=product.id))
				OR (collection.mode='dynamic' AND collection_rules_match_product(product.id,collection.rules)))),ARRAY[]::TEXT[]),
		COALESCE((SELECT AVG(rating)::float8 FROM product_reviews review WHERE review.product_id=product.id AND review.status='published'),0),
		(SELECT COUNT(*) FROM product_reviews review WHERE review.product_id=product.id AND review.status='published'),
		COALESCE((
			WITH RECURSIVE ancestors AS (
				SELECT id,parent_id,0 depth FROM categories WHERE id=product.category_id
				UNION ALL SELECT category.id,category.parent_id,ancestor.depth+1 FROM categories category JOIN ancestors ancestor ON ancestor.parent_id=category.id
			), effective AS (
				SELECT DISTINCT ON (definition.id) definition.id,definition.code,definition.name,definition.unit,
					definition.value_scope,definition.data_type,assignment.sort_order,assignment.is_filterable,assignment.is_badge,
					assignment.is_excluded,ancestor.depth
				FROM ancestors ancestor JOIN category_attributes assignment ON assignment.category_id=ancestor.id
				JOIN attribute_definitions definition ON definition.id=assignment.attribute_id AND definition.audience='customer' AND definition.is_active
				ORDER BY definition.id,ancestor.depth
			), values AS (
				SELECT effective.code,
					COALESCE((SELECT filter.title FROM catalog_filters filter WHERE filter.attribute_id=effective.id AND filter.is_active AND (filter.category_id IS NULL OR EXISTS(SELECT 1 FROM ancestors ancestor WHERE ancestor.id=filter.category_id)) ORDER BY filter.category_id NULLS LAST,filter.sort_order,filter.id LIMIT 1),effective.name) AS name,
					effective.unit,effective.data_type,
					COALESCE((SELECT filter.display_mode FROM catalog_filters filter WHERE filter.attribute_id=effective.id AND filter.is_active AND (filter.category_id IS NULL OR EXISTS(SELECT 1 FROM ancestors ancestor WHERE ancestor.id=filter.category_id)) ORDER BY filter.category_id NULLS LAST,filter.sort_order,filter.id LIMIT 1),'select') AS display_mode,
					effective.sort_order,effective.is_filterable,effective.is_badge,value.value
				FROM effective JOIN product_attribute_values value ON value.attribute_id=effective.id AND value.product_id=product.id
				WHERE effective.value_scope='product' AND NOT effective.is_excluded AND (effective.is_badge OR (effective.is_filterable AND EXISTS(SELECT 1 FROM catalog_filters filter WHERE filter.attribute_id=effective.id AND filter.is_active AND (filter.category_id IS NULL OR EXISTS(SELECT 1 FROM ancestors ancestor WHERE ancestor.id=filter.category_id)))))
				UNION ALL
				SELECT effective.code,
					COALESCE((SELECT filter.title FROM catalog_filters filter WHERE filter.attribute_id=effective.id AND filter.is_active AND (filter.category_id IS NULL OR EXISTS(SELECT 1 FROM ancestors ancestor WHERE ancestor.id=filter.category_id)) ORDER BY filter.category_id NULLS LAST,filter.sort_order,filter.id LIMIT 1),effective.name) AS name,
					effective.unit,effective.data_type,
					COALESCE((SELECT filter.display_mode FROM catalog_filters filter WHERE filter.attribute_id=effective.id AND filter.is_active AND (filter.category_id IS NULL OR EXISTS(SELECT 1 FROM ancestors ancestor WHERE ancestor.id=filter.category_id)) ORDER BY filter.category_id NULLS LAST,filter.sort_order,filter.id LIMIT 1),'select') AS display_mode,
					effective.sort_order,effective.is_filterable,effective.is_badge,value.value
				FROM effective JOIN variant_attribute_values value ON value.attribute_id=effective.id
				JOIN product_variants variant ON variant.id=value.variant_id AND variant.product_id=product.id AND variant.is_active=1
				WHERE effective.value_scope='variant' AND NOT effective.is_excluded AND (effective.is_badge OR (effective.is_filterable AND EXISTS(SELECT 1 FROM catalog_filters filter WHERE filter.attribute_id=effective.id AND filter.is_active AND (filter.category_id IS NULL OR EXISTS(SELECT 1 FROM ancestors ancestor WHERE ancestor.id=filter.category_id)))))
			)
			SELECT jsonb_agg(jsonb_build_object(
				'code',value.code,'name',value.name,'unit',value.unit,'value',value.value,
				'badge',value.is_badge,'filterable',value.is_filterable,
				'dataType',value.data_type,'displayMode',value.display_mode,
				'showInCharacteristics',true)
				ORDER BY value.sort_order,value.code)
			FROM values value
		), '[]'::jsonb)
	FROM products product
	JOIN default_variants default_variant ON default_variant.product_id=product.id
	LEFT JOIN LATERAL (
		WITH RECURSIVE ancestors AS (
			SELECT id,parent_id,name FROM categories WHERE id=product.category_id
			UNION ALL
			SELECT category.id,category.parent_id,category.name
			FROM categories category JOIN ancestors ancestor ON ancestor.parent_id=category.id
		)
		SELECT name FROM ancestors WHERE parent_id IS NULL LIMIT 1
	) root_category ON TRUE
	LEFT JOIN popularity ON popularity.product_id=product.id
	WHERE product.status='published'
	ORDER BY default_variant.stock>0 DESC,product.is_featured DESC,product.name ASC
	LIMIT 1000
`

func (repository *PostgresRepository) ListAvailable(ctx context.Context) ([]Product, error) {
	rows, err := repository.pool.Query(ctx, catalogListQuery)
	if err != nil {
		return nil, fmt.Errorf("query catalog: %w", err)
	}
	defer rows.Close()
	products := make([]Product, 0)
	for rows.Next() {
		var product Product
		var priceMinor int64
		var filterAttributes []byte
		if err := rows.Scan(&product.ID, &product.SKU, &product.Name, &product.Latin, &product.Category, &priceMinor,
			&product.Image, &product.Size, &product.Stock, &product.CatalogSection, &product.PlantKind, &product.LightLevel,
			&product.Watering, &product.HeightClass, &product.CareLevel, &product.Placement, &product.PetSafety, &product.GrowthHabit,
			&product.CategoryID, &product.PopularityScore, &product.Collections, &product.Rating, &product.ReviewsCount, &filterAttributes); err != nil {
			return nil, fmt.Errorf("scan catalog product: %w", err)
		}
		product.Price = float64(priceMinor) / 100
		product.Light = "Уточните у консультанта"
		if err := json.Unmarshal(filterAttributes, &product.FilterAttributes); err != nil {
			return nil, fmt.Errorf("decode filter attributes: %w", err)
		}
		products = append(products, product)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read catalog rows: %w", err)
	}
	return products, nil
}

func (repository *PostgresRepository) ListFeedOffers(ctx context.Context) ([]FeedOffer, error) {
	rows, err := repository.pool.Query(ctx, `
		WITH RECURSIVE category_paths AS (
			SELECT id,parent_id,name,name::TEXT AS path FROM categories WHERE parent_id IS NULL
			UNION ALL
			SELECT child.id,child.parent_id,child.name,(parent.path || ' > ' || child.name)::TEXT
			FROM categories child JOIN category_paths parent ON parent.id=child.parent_id
		)
		SELECT product.product_code::TEXT,variant.sku,product.name,variant.label,
			COALESCE(NULLIF(product.short_description,''),NULLIF(product.description,''),''),
			variant.base_price_minor,
			COALESCE((SELECT SUM(GREATEST(item.available_qty-item.reserved_qty,0)) FROM inventory i WHERE i.variant_id=variant.id),0)::INTEGER,
			COALESCE((
				SELECT COALESCE(mirror.large_url,mirror.card_url,media.object_key)
				FROM product_media media LEFT JOIN media_mirror mirror ON mirror.source_url=media.object_key
				WHERE media.product_id=product.id AND (media.variant_id=variant.id OR media.variant_id IS NULL)
				ORDER BY (media.variant_id=variant.id) DESC,media.is_primary DESC,media.sort_order,media.id LIMIT 1
			),''),
			COALESCE(product.category_id,0),COALESCE(category.path,'Каталог'),
			COUNT(*) OVER (PARTITION BY product.id)::INTEGER
		FROM products product
		JOIN product_variants variant ON variant.product_id=product.id AND variant.is_active=1 AND variant.archived_at IS NULL
		LEFT JOIN category_paths category ON category.id=product.category_id
		WHERE product.status='published'
		ORDER BY product.product_code,variant.id
	`)
	if err != nil {
		return nil, fmt.Errorf("query feed offers: %w", err)
	}
	defer rows.Close()
	offers := make([]FeedOffer, 0)
	for rows.Next() {
		var offer FeedOffer
		var priceMinor int64
		if err := rows.Scan(&offer.ProductCode, &offer.SKU, &offer.Name, &offer.Label, &offer.Description,
			&priceMinor, &offer.Stock, &offer.Image, &offer.CategoryID, &offer.Category, &offer.VariantCount); err != nil {
			return nil, fmt.Errorf("scan feed offer: %w", err)
		}
		offer.Price = float64(priceMinor) / 100
		offers = append(offers, offer)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read feed offers: %w", err)
	}
	return offers, nil
}

type PackageSize struct {
	LengthCM    int
	WidthCM     int
	HeightCM    int
	WeightGrams int
}

// PackageSizes is SKU-based. Delivery must price the exact sellable variant,
// never the first size that happens to share a product card.
//
// Sizes come from PIM, not from the legacy product_variants columns: since
// migration 061 variant_attribute_values is the single source, so a parcel
// edited by an operator changes the delivery price immediately.
func (repository *PostgresRepository) PackageSizes(ctx context.Context, skus []string) (map[string]PackageSize, error) {
	sizes := make(map[string]PackageSize, len(skus))
	if len(skus) == 0 {
		return sizes, nil
	}
	rows, err := repository.pool.Query(ctx, `
		SELECT variant.sku,
			COALESCE(variant_numeric_attribute(variant.id,'package_length_cm'),0)::INTEGER,
			COALESCE(variant_numeric_attribute(variant.id,'package_width_cm'),0)::INTEGER,
			COALESCE(variant_numeric_attribute(variant.id,'package_height_cm'),0)::INTEGER,
			COALESCE(variant_numeric_attribute(variant.id,'package_weight_grams'),0)::INTEGER
		FROM product_variants variant
		JOIN products product ON product.id=variant.product_id AND product.status='published'
		WHERE variant.sku=ANY($1) AND variant.is_active=1
	`, skus)
	if err != nil {
		return nil, fmt.Errorf("load package sizes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var sku string
		var size PackageSize
		if err := rows.Scan(&sku, &size.LengthCM, &size.WidthCM, &size.HeightCM, &size.WeightGrams); err != nil {
			return nil, fmt.Errorf("scan package size: %w", err)
		}
		sizes[sku] = size
	}
	return sizes, rows.Err()
}

func (repository *PostgresRepository) ListCollections(ctx context.Context) ([]Collection, error) {
	rows, err := repository.pool.Query(ctx, `
		SELECT collection.slug,collection.title,collection.note,collection.cover_url,COUNT(product.id)::INTEGER
		FROM collections collection
		JOIN products product ON product.status='published' AND ((collection.mode='manual' AND EXISTS(SELECT 1 FROM collection_products member WHERE member.collection_id=collection.id AND member.product_id=product.id))
			OR (collection.mode='dynamic' AND collection_rules_match_product(product.id,collection.rules)))
		WHERE collection.is_active=1 GROUP BY collection.id HAVING COUNT(product.id)>0
		ORDER BY collection.sort_order,collection.id
	`)
	if err != nil {
		return nil, fmt.Errorf("query collections: %w", err)
	}
	defer rows.Close()
	collections := make([]Collection, 0)
	for rows.Next() {
		var item Collection
		if err := rows.Scan(&item.Slug, &item.Title, &item.Note, &item.CoverURL, &item.Count); err != nil {
			return nil, fmt.Errorf("scan collection: %w", err)
		}
		collections = append(collections, item)
	}
	return collections, rows.Err()
}
