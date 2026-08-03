package catalog

import (
	"context"
	"errors"
	"fmt"

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
		SELECT id, slug, name, latin_name, short_description, description, care_instructions
		FROM products WHERE slug = $1 AND status = 'published' LIMIT 1
	`, slug).Scan(&productID, &detail.ID, &detail.Name, &detail.Latin,
		&detail.ShortDescription, &detail.Description, &detail.CareInstructions)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProductDetail{}, ErrNotFound
	}
	if err != nil {
		return ProductDetail{}, fmt.Errorf("query product detail: %w", err)
	}

	mediaRows, err := repository.pool.Query(ctx, `
		SELECT object_key FROM product_media WHERE product_id = $1
		ORDER BY is_primary DESC, sort_order, id
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

	available, err := repository.ListAvailable(ctx)
	if err != nil {
		return ProductDetail{}, err
	}
	detail.Recommendations = []Product{}
	for _, item := range available {
		if item.ID != slug {
			detail.Recommendations = append(detail.Recommendations, item)
			if len(detail.Recommendations) == 4 {
				break
			}
		}
	}
	return detail, nil
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (repository *PostgresRepository) ListAvailable(ctx context.Context) ([]Product, error) {
	const query = `
		SELECT
			p.slug,
			p.name,
			p.latin_name,
			'Растения',
			pv.base_price_minor,
			COALESCE(
				(
					SELECT pm.object_key
					FROM product_media pm
					WHERE pm.product_id = p.id
					ORDER BY pm.is_primary DESC, pm.sort_order ASC
					LIMIT 1
				),
				'/assets/hero-monstera.png'
			),
			pv.label,
			COALESCE(SUM(GREATEST(i.available_qty - i.reserved_qty, 0)), 0)
		FROM products p
		JOIN product_variants pv ON pv.product_id = p.id AND pv.is_active = 1
		LEFT JOIN inventory i ON i.variant_id = pv.id
		WHERE p.status = 'published'
		GROUP BY p.id, pv.id
		HAVING COALESCE(SUM(GREATEST(i.available_qty - i.reserved_qty, 0)), 0) > 0
		ORDER BY p.is_featured DESC, p.name ASC
		LIMIT 1000
	`

	rows, err := repository.pool.Query(ctx, query)
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
