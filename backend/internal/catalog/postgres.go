package catalog

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
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
