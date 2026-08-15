package catalog

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func (repository *PostgresRepository) DetailBySlug(ctx context.Context, slug string) (ProductDetail, error) {
	var detail ProductDetail
	var productID int64
	err := repository.pool.QueryRow(ctx, `
		SELECT id, slug, name, latin_name, short_description, description, care_instructions,
			catalog_section, COALESCE(plant_kind, ''), COALESCE(light_level, ''),
			COALESCE(watering, ''), COALESCE(height_class, ''), COALESCE(care_level, ''),
			COALESCE(placement, ''), COALESCE(pet_safety, ''), COALESCE(growth_habit, ''), category_id,
			plant_passport, important_warnings,
			COALESCE((SELECT AVG(rating)::float8 FROM product_reviews WHERE product_id = products.id AND status = 'published'), 0),
			(SELECT COUNT(*) FROM product_reviews WHERE product_id = products.id AND status = 'published')
		FROM products WHERE slug = $1 AND status = 'published' LIMIT 1
	`, slug).Scan(&productID, &detail.ID, &detail.Name, &detail.Latin,
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
		-- Своя копия снимка, если она уже перенесена, иначе ссылка
		-- поставщика: витрина не должна пустеть, пока идёт перенос.
		SELECT COALESCE(mirror.large_url, media.object_key)
		FROM product_media media
		LEFT JOIN media_mirror mirror ON mirror.source_url = media.object_key
		WHERE media.product_id = $1
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
	if len(detail.Images) == 0 {
		detail.Images = append(detail.Images, "/assets/hero-monstera.png")
	}

	variantRows, err := repository.pool.Query(ctx, `
		SELECT pv.id, pv.sku, pv.label, pv.base_price_minor,
			COALESCE(SUM(GREATEST(i.available_qty - i.reserved_qty, 0)), 0)::INTEGER,
			pv.height_cm, pv.pot_diameter_cm, pv.wholesale_min_qty
		FROM product_variants pv LEFT JOIN inventory i ON i.variant_id = pv.id
		WHERE pv.product_id = $1 AND pv.is_active = 1
		GROUP BY pv.id ORDER BY pv.id
	`, productID)
	if err != nil {
		return ProductDetail{}, fmt.Errorf("query product variants: %w", err)
	}
	detail.Variants = []Variant{}
	for variantRows.Next() {
		var variant Variant
		var priceMinor int64
		if err := variantRows.Scan(&variant.ID, &variant.SKU, &variant.Label, &priceMinor,
			&variant.Stock, &variant.HeightCM, &variant.PotDiameterCM,
			&variant.WholesaleMinQty); err != nil {
			variantRows.Close()
			return ProductDetail{}, err
		}
		variant.Price = float64(priceMinor) / 100
		detail.Variants = append(detail.Variants, variant)
	}
	if err := variantRows.Err(); err != nil {
		variantRows.Close()
		return ProductDetail{}, fmt.Errorf("read product variants: %w", err)
	}
	variantRows.Close()
	reviewRows, err := repository.pool.Query(ctx, `
		SELECT r.id, r.rating, r.body, COALESCE(NULLIF(c.full_name, ''), 'Покупатель'),
			to_char(r.created_at, 'YYYY-MM-DD'), true
		FROM product_reviews r JOIN customers c ON c.id = r.customer_id
		WHERE r.product_id = $1 AND r.status = 'published' ORDER BY r.created_at DESC LIMIT 30`, productID)
	if err != nil { return ProductDetail{}, fmt.Errorf("query reviews: %w", err) }
	detail.Reviews = []Review{}
	for reviewRows.Next() { var review Review; if err := reviewRows.Scan(&review.ID, &review.Rating, &review.Text, &review.Author, &review.Date, &review.VerifiedPurchase); err != nil { reviewRows.Close(); return ProductDetail{}, err }; mediaRows, _ := repository.pool.Query(ctx, `SELECT '/api/v1/review-photos/' || id, content_type FROM product_review_photos WHERE review_id=$1 ORDER BY sort_order,id`, review.ID); for mediaRows != nil && mediaRows.Next() { var media ReviewMedia; _ = mediaRows.Scan(&media.URL, &media.ContentType); review.Media = append(review.Media, media); if strings.HasPrefix(media.ContentType, "image/") { review.Photos = append(review.Photos, media.URL) } }; if mediaRows != nil { mediaRows.Close() }; detail.Reviews = append(detail.Reviews, review) }
	reviewRows.Close()

	available, err := repository.ListAvailable(ctx)
	if err != nil {
		return ProductDetail{}, err
	}
	type scoredProduct struct {
		product Product
		score   int
	}
	candidates := make([]scoredProduct, 0, len(available))
	for _, item := range available {
		if item.ID != slug {
			candidates = append(candidates, scoredProduct{product: item, score: recommendationScore(detail, item)})
		}
	}
	sort.SliceStable(candidates, func(left, right int) bool {
		return candidates[left].score > candidates[right].score
	})
	detail.Recommendations = []Product{}
	for _, candidate := range candidates {
		detail.Recommendations = append(detail.Recommendations, candidate.product)
		if len(detail.Recommendations) == 4 {
			break
		}
	}
	return detail, nil
}

func recommendationScore(current ProductDetail, candidate Product) int {
	score := 0
	if current.CatalogSection == candidate.CatalogSection {
		score += 6
	}
	if current.CategoryID != nil && candidate.CategoryID != nil && *current.CategoryID == *candidate.CategoryID {
		score += 8
	}
	pairs := [][2]string{
		{current.PlantKind, candidate.PlantKind},
		{current.LightLevel, candidate.LightLevel},
		{current.Watering, candidate.Watering},
		{current.HeightClass, candidate.HeightClass},
		{current.CareLevel, candidate.CareLevel},
		{current.Placement, candidate.Placement},
		{current.PetSafety, candidate.PetSafety},
		{current.GrowthHabit, candidate.GrowthHabit},
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
	rows, err := repository.pool.Query(ctx, `
		SELECT id, parent_id, name, slug, sort_order, icon
		FROM categories WHERE active = 1 ORDER BY sort_order, name
	`)
	if err != nil { return nil, fmt.Errorf("query categories: %w", err) }
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
			SELECT oi.product_id, SUM(oi.quantity * CASE
				WHEN o.created_at >= CURRENT_TIMESTAMP - INTERVAL '30 days' THEN 1.0
				WHEN o.created_at >= CURRENT_TIMESTAMP - INTERVAL '90 days' THEN 0.5
				WHEN o.created_at >= CURRENT_TIMESTAMP - INTERVAL '365 days' THEN 0.2
				ELSE 0.05 END)::DOUBLE PRECISION AS score
			FROM order_items oi JOIN orders o ON o.id = oi.order_id
			WHERE o.status <> 'cancelled'
			  AND (o.status = 'completed' OR o.payment_status = 'paid')
			GROUP BY oi.product_id
		)
		SELECT
			p.slug,
			p.name,
			p.latin_name,
			'Растения',
			pv.base_price_minor,
			COALESCE(
				(
					SELECT COALESCE(mm.card_url, pm.object_key)
					FROM product_media pm
					LEFT JOIN media_mirror mm ON mm.source_url = pm.object_key
					WHERE pm.product_id = p.id
					ORDER BY pm.is_primary DESC, pm.sort_order ASC
					LIMIT 1
				),
				'/assets/hero-monstera.png'
			),
			pv.label,
			COALESCE(SUM(GREATEST(i.available_qty - i.reserved_qty, 0)), 0),
			p.catalog_section, COALESCE(p.plant_kind, ''), COALESCE(p.light_level, ''),
			COALESCE(p.watering, ''), COALESCE(p.height_class, ''),
			COALESCE(p.care_level, ''), COALESCE(p.placement, ''),
			COALESCE(p.pet_safety, ''), COALESCE(p.growth_habit, ''), p.category_id,
			COALESCE(popularity.score, 0),
			COALESCE((
				SELECT ARRAY_AGG(c.slug ORDER BY c.sort_order, c.id)
				FROM collection_products cp
				JOIN collections c ON c.id = cp.collection_id AND c.is_active = 1
				WHERE cp.product_id = p.id
			), ARRAY[]::TEXT[]),
			COALESCE((SELECT AVG(rating)::float8 FROM product_reviews r WHERE r.product_id=p.id AND r.status='published'),0),
			(SELECT COUNT(*) FROM product_reviews r WHERE r.product_id=p.id AND r.status='published')
		FROM products p
		JOIN product_variants pv ON pv.product_id = p.id AND pv.is_active = 1
		LEFT JOIN inventory i ON i.variant_id = pv.id
		LEFT JOIN popularity ON popularity.product_id = p.slug
		WHERE p.status = 'published'
		GROUP BY p.id, pv.id, popularity.score
		ORDER BY
			COALESCE(SUM(GREATEST(i.available_qty - i.reserved_qty, 0)), 0) > 0 DESC,
			p.is_featured DESC, p.name ASC
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
		if err := rows.Scan(
			&product.ID,
			&product.Name,
			&product.Latin,
			&product.Category,
			&priceMinor,
			&product.Image,
			&product.Size,
			&product.Stock,
			&product.CatalogSection,
			&product.PlantKind,
			&product.LightLevel,
			&product.Watering,
			&product.HeightClass,
			&product.CareLevel,
			&product.Placement,
			&product.PetSafety,
			&product.GrowthHabit,
			&product.CategoryID,
			&product.PopularityScore,
			&product.Collections,
			&product.Rating,
			&product.ReviewsCount,
		); err != nil {
			return nil, fmt.Errorf("scan catalog product: %w", err)
		}
		product.Price = float64(priceMinor) / 100
		product.Light = "Уточните у консультанта"
		products = append(products, product)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read catalog rows: %w", err)
	}

	return products, nil
}

// PackageSize is how a single item is boxed, in centimetres and grams. A
// zero value means the manager has not measured it yet.
type PackageSize struct {
	LengthCM    int
	WidthCM     int
	HeightCM    int
	WeightGrams int
}

// PackageSizes returns the box of each requested product. Products with
// nothing filled in come back as a zero value, so the caller can decide what
// to assume rather than being handed a guess.
func (repository *PostgresRepository) PackageSizes(
	ctx context.Context,
	slugs []string,
) (map[string]PackageSize, error) {
	sizes := make(map[string]PackageSize, len(slugs))
	if len(slugs) == 0 {
		return sizes, nil
	}
	rows, err := repository.pool.Query(ctx, `
		SELECT DISTINCT ON (p.slug)
			p.slug,
			COALESCE(pv.package_length_cm, 0),
			COALESCE(pv.package_width_cm, 0),
			COALESCE(pv.package_height_cm, 0),
			COALESCE(pv.package_weight_grams, 0)
		FROM products p
		JOIN product_variants pv ON pv.product_id = p.id AND pv.is_active = 1
		WHERE p.slug = ANY($1)
		ORDER BY p.slug, pv.id
	`, slugs)
	if err != nil {
		return nil, fmt.Errorf("load package sizes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var slug string
		var size PackageSize
		if err := rows.Scan(
			&slug,
			&size.LengthCM,
			&size.WidthCM,
			&size.HeightCM,
			&size.WeightGrams,
		); err != nil {
			return nil, fmt.Errorf("scan package size: %w", err)
		}
		sizes[slug] = size
	}
	return sizes, rows.Err()
}

// ListCollections returns the hand-made collections the storefront shows as
// tabs. Empty ones are left out: a tab that leads to nothing is a dead end.
func (repository *PostgresRepository) ListCollections(ctx context.Context) ([]Collection, error) {
	rows, err := repository.pool.Query(ctx, `
		SELECT c.slug, c.title, c.note, COUNT(cp.product_id)::INTEGER
		FROM collections c
		JOIN collection_products cp ON cp.collection_id = c.id
		JOIN products p ON p.id = cp.product_id AND p.status = 'published'
		WHERE c.is_active = 1
		GROUP BY c.id
		HAVING COUNT(cp.product_id) > 0
		ORDER BY c.sort_order, c.id
	`)
	if err != nil {
		return nil, fmt.Errorf("query collections: %w", err)
	}
	defer rows.Close()
	collections := make([]Collection, 0)
	for rows.Next() {
		var item Collection
		if err := rows.Scan(&item.Slug, &item.Title, &item.Note, &item.Count); err != nil {
			return nil, fmt.Errorf("scan collection: %w", err)
		}
		collections = append(collections, item)
	}
	return collections, rows.Err()
}
