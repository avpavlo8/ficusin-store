package saby

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	pool     *pgxpool.Pool
	verifier *OIDCVerifier
}

type normalizedItem struct {
	id          string
	article     string
	name        string
	description string
	costMinor   int64
	balance     int
	images      []string
}

func NewService(pool *pgxpool.Pool, verifier *OIDCVerifier) *Service {
	return &Service{pool: pool, verifier: verifier}
}

func (service *Service) Verify(ctx context.Context, token string) error {
	return service.verifier.Verify(ctx, token)
}

func (service *Service) Sync(ctx context.Context, items []CatalogItem) (Result, error) {
	sourceItems := normalizeItems(items)
	if len(sourceItems) == 0 {
		return Result{}, errors.New("empty Saby catalog")
	}

	var syncRunID int64
	if err := service.pool.QueryRow(ctx, `
		INSERT INTO sync_runs (source, direction, status, items_read)
		VALUES ('saby', 'import', 'running', $1)
		RETURNING id
	`, len(sourceItems)).Scan(&syncRunID); err != nil {
		return Result{}, fmt.Errorf("start Saby sync: %w", err)
	}

	if err := service.sync(ctx, sourceItems); err != nil {
		summary := err.Error()
		if len(summary) > 500 {
			summary = summary[:500]
		}
		_, markErr := service.pool.Exec(ctx, `
			UPDATE sync_runs
			SET status = 'failed', errors_count = 1, error_summary = $1,
				finished_at = CURRENT_TIMESTAMP
			WHERE id = $2
		`, summary, syncRunID)
		if markErr != nil {
			return Result{}, fmt.Errorf("%w; mark failed sync: %v", err, markErr)
		}
		return Result{}, err
	}

	if _, err := service.pool.Exec(ctx, `
		UPDATE sync_runs
		SET status = 'success', items_created = $1, items_updated = $1,
			finished_at = CURRENT_TIMESTAMP
		WHERE id = $2
	`, len(sourceItems), syncRunID); err != nil {
		return Result{}, fmt.Errorf("finish Saby sync: %w", err)
	}
	return Result{
		OK: true, ItemsRead: len(sourceItems), SyncedAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func (service *Service) sync(ctx context.Context, items []normalizedItem) error {
	tx, err := service.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin Saby sync: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		INSERT INTO warehouses (saby_id, name, city, address, is_active)
		VALUES ('saby-ryazan-main', 'Основной склад', 'Рязань', 'Новосёлов, 40А', 1)
		ON CONFLICT (saby_id) DO UPDATE SET
			name = EXCLUDED.name, city = EXCLUDED.city,
			address = EXCLUDED.address, is_active = 1
	`); err != nil {
		return fmt.Errorf("upsert Saby warehouse: %w", err)
	}

	articleCounts := make(map[string]int)
	for _, item := range items {
		if item.article != "" {
			articleCounts[item.article]++
		}
	}

	receivedIDs := make([]string, 0, len(items))
	for _, item := range items {
		var productID int64
		if err := tx.QueryRow(ctx, `
			INSERT INTO products (
				saby_id, name, slug, description, search_text, status,
				saby_updated_at, updated_at
			)
			VALUES ($1, $2, $3, $4, $5, 'published', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
			ON CONFLICT (saby_id) DO UPDATE SET
				name = EXCLUDED.name,
				description = EXCLUDED.description,
				search_text = EXCLUDED.search_text,
				status = 'published',
				saby_updated_at = CURRENT_TIMESTAMP,
				updated_at = CURRENT_TIMESTAMP
			RETURNING id
		`, item.id, item.name, "saby-"+item.id, item.description,
			strings.TrimSpace(item.name+" "+item.article)).Scan(&productID); err != nil {
			return fmt.Errorf("upsert Saby product %s: %w", item.id, err)
		}

		sku := item.article
		if sku == "" || articleCounts[sku] != 1 {
			if sku == "" {
				sku = "SABY"
			}
			sku += "-" + item.id
		}
		var variantID int64
		if err := tx.QueryRow(ctx, `
			INSERT INTO product_variants (
				product_id, saby_id, sku, label, base_price_minor,
				is_active, saby_updated_at, updated_at
			)
			VALUES ($1, $2, $3, 'Основной размер', $4, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
			ON CONFLICT (saby_id) DO UPDATE SET
				product_id = EXCLUDED.product_id,
				sku = EXCLUDED.sku,
				base_price_minor = EXCLUDED.base_price_minor,
				is_active = 1,
				saby_updated_at = CURRENT_TIMESTAMP,
				updated_at = CURRENT_TIMESTAMP
			RETURNING id
		`, productID, item.id, sku, item.costMinor).Scan(&variantID); err != nil {
			return fmt.Errorf("upsert Saby variant %s: %w", item.id, err)
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO inventory (
				warehouse_id, variant_id, available_qty, reserved_qty, synced_at
			)
			SELECT id, $1, $2, 0, CURRENT_TIMESTAMP
			FROM warehouses WHERE saby_id = 'saby-ryazan-main'
			ON CONFLICT (warehouse_id, variant_id) DO UPDATE SET
				available_qty = EXCLUDED.available_qty,
				synced_at = CURRENT_TIMESTAMP
		`, variantID, item.balance); err != nil {
			return fmt.Errorf("upsert Saby inventory %s: %w", item.id, err)
		}

		if _, err := tx.Exec(ctx, "DELETE FROM product_media WHERE product_id = $1", productID); err != nil {
			return fmt.Errorf("replace Saby media %s: %w", item.id, err)
		}
		for index, image := range item.images {
			if _, err := tx.Exec(ctx, `
				INSERT INTO product_media (
					product_id, object_key, alt_text, sort_order, is_primary
				) VALUES ($1, $2, $3, $4, $5)
			`, productID, image, item.name, index, boolToInt(index == 0)); err != nil {
				return fmt.Errorf("insert Saby media %s: %w", item.id, err)
			}
		}
		receivedIDs = append(receivedIDs, item.id)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE products
		SET status = 'archived', updated_at = CURRENT_TIMESTAMP
		WHERE saby_id IS NOT NULL AND NOT (saby_id = ANY($1::text[]))
	`, receivedIDs); err != nil {
		return fmt.Errorf("archive missing Saby products: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit Saby sync: %w", err)
	}
	return nil
}

func normalizeItems(items []CatalogItem) []normalizedItem {
	result := make([]normalizedItem, 0, len(items))
	for _, item := range items {
		id := valueString(item.ID)
		cost, costOK := valueFloat(item.Cost)
		name := strings.TrimSpace(item.Name)
		if item.IsParent || (item.Published != nil && !*item.Published) ||
			id == "" || name == "" || !costOK {
			continue
		}
		balance, _ := valueFloat(item.Balance)
		images := make([]string, 0, min(len(item.Images), 8))
		for _, value := range item.Images {
			if image := resolveImage(value); image != "" {
				images = append(images, image)
				if len(images) == 8 {
					break
				}
			}
		}
		result = append(result, normalizedItem{
			id: id, article: strings.TrimSpace(item.Article), name: name,
			description: strings.TrimSpace(item.Description),
			costMinor:   max(0, int64(math.Round(cost*100))),
			balance:     max(0, int(math.Floor(balance))),
			images:      images,
		})
	}
	return result
}

func valueString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return ""
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case json.Number:
		return typed.String()
	default:
		return ""
	}
}

func valueFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, !math.IsNaN(typed) && !math.IsInf(typed, 0)
	case string:
		parsed, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(typed), ",", "."), 64)
		return parsed, err == nil && !math.IsNaN(parsed) && !math.IsInf(parsed, 0)
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil && !math.IsNaN(parsed) && !math.IsInf(parsed, 0)
	default:
		return 0, false
	}
}

func resolveImage(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return ""
	}
	if !parsed.IsAbs() {
		base, _ := url.Parse("https://online.sbis.ru")
		parsed = base.ResolveReference(parsed)
	}
	if parsed.Path == "/img" {
		if encoded := parsed.Query().Get("params"); encoded != "" {
			decoded, decodeErr := base64.RawURLEncoding.DecodeString(strings.TrimRight(encoded, "="))
			if decodeErr == nil {
				var parameters struct {
					PhotoURL string `json:"PhotoURL"`
				}
				if json.Unmarshal(decoded, &parameters) == nil && strings.TrimSpace(parameters.PhotoURL) != "" {
					if photo, photoErr := url.Parse(strings.TrimSpace(parameters.PhotoURL)); photoErr == nil {
						parsed = photo
					}
				}
			}
		}
	}
	if parsed.Scheme == "http" {
		parsed.Scheme = "https"
	}
	if parsed.Scheme != "https" || parsed.Host == "" {
		return ""
	}
	return parsed.String()
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
