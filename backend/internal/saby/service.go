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

// normalizedItem — позиция номенклатуры, приведённая к нашему виду.
type normalizedItem struct {
	id          string
	code        string
	article     string
	barcode     string
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

// poolRow — строка справочника номенклатуры в том виде, в каком она уезжает
// в базу одним запросом.
type poolRow struct {
	SabyID      string   `json:"saby_id"`
	Code        string   `json:"code"`
	Article     string   `json:"article"`
	Barcode     string   `json:"barcode"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	PriceMinor  int64    `json:"price_minor"`
	Balance     int      `json:"balance"`
	Images      []string `json:"images"`
}

// sync складывает выгрузку в справочник и обновляет у товаров ровно то, что
// им разрешено брать из СБИС.
//
// Карточками распоряжается магазин. Обмен больше не заводит товары сам, не
// переписывает названия и не убирает с витрины то, чего не оказалось в
// выгрузке: у пропавшей позиции обнуляется остаток, а карточка, ссылки на
// неё и место в поиске остаются. Из СБИС по умолчанию приходит только
// остаток; название, описание, цена и фотографии — лишь если это поле
// отмечено у товара.
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

	rows := make([]poolRow, 0, len(items))
	received := make([]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, poolRow{
			SabyID: item.id, Code: item.code, Article: item.article, Barcode: item.barcode,
			Name: item.name, Description: item.description,
			PriceMinor: item.costMinor, Balance: item.balance, Images: item.images,
		})
		received = append(received, item.id)
	}
	catalogue, err := json.Marshal(rows)
	if err != nil {
		return fmt.Errorf("pack Saby catalogue: %w", err)
	}

	// Справочник держит всю номенклатуру, включая то, чего на витрине нет:
	// из него менеджер выбирает, что завести в магазин, и импорт по кодам
	// ищет здесь, а не ходит в СБИС в момент нажатия кнопки.
	if _, err := tx.Exec(ctx, `
		INSERT INTO saby_nomenclature (
			saby_id, code, article, barcode, name, description,
			price_minor, balance, images, seen_at, missing_since
		)
		SELECT item.saby_id, item.code, item.article, item.barcode, item.name,
			item.description, item.price_minor, item.balance,
			ARRAY(SELECT jsonb_array_elements_text(item.images)),
			CURRENT_TIMESTAMP, NULL
		FROM jsonb_to_recordset($1::jsonb) AS item(
			saby_id TEXT, code TEXT, article TEXT, barcode TEXT, name TEXT,
			description TEXT, price_minor BIGINT, balance INTEGER, images JSONB
		)
		ON CONFLICT (saby_id) DO UPDATE SET
			code = EXCLUDED.code, article = EXCLUDED.article,
			barcode = EXCLUDED.barcode, name = EXCLUDED.name,
			description = EXCLUDED.description, price_minor = EXCLUDED.price_minor,
			balance = EXCLUDED.balance, images = EXCLUDED.images,
			seen_at = CURRENT_TIMESTAMP, missing_since = NULL
	`, catalogue); err != nil {
		return fmt.Errorf("upsert Saby nomenclature: %w", err)
	}

	// Пропавшее из выгрузки не удаляем и не архивируем: обнуляем остаток и
	// запоминаем день пропажи, чтобы было видно, с каких пор товара нет.
	if _, err := tx.Exec(ctx, `
		UPDATE saby_nomenclature
		SET balance = 0, missing_since = COALESCE(missing_since, CURRENT_TIMESTAMP)
		WHERE NOT (saby_id = ANY($1::text[]))
	`, received); err != nil {
		return fmt.Errorf("mark missing Saby items: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO inventory (warehouse_id, variant_id, available_qty, reserved_qty, synced_at)
		SELECT warehouse.id, pv.id, source.balance, 0, CURRENT_TIMESTAMP
		FROM product_variants pv
		JOIN products p ON p.id = pv.product_id
		JOIN saby_nomenclature source ON source.saby_id = pv.saby_id
		CROSS JOIN (SELECT id FROM warehouses WHERE saby_id = 'saby-ryazan-main') warehouse
		WHERE 'stock' = ANY(p.saby_fields)
		ON CONFLICT (warehouse_id, variant_id) DO UPDATE SET
			available_qty = EXCLUDED.available_qty,
			synced_at = CURRENT_TIMESTAMP
	`); err != nil {
		return fmt.Errorf("update Saby stock: %w", err)
	}

	// Текстовые поля у пропавшей позиции не трогаем: пустое название взамен
	// прежнего — это потеря, а не обновление.
	if _, err := tx.Exec(ctx, `
		UPDATE products p
		SET name = source.name, updated_at = CURRENT_TIMESTAMP
		FROM saby_nomenclature source
		WHERE source.saby_id = p.saby_id AND 'name' = ANY(p.saby_fields)
			AND source.missing_since IS NULL AND source.name <> '' AND p.name <> source.name
	`); err != nil {
		return fmt.Errorf("update Saby names: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE products p
		SET description = source.description, updated_at = CURRENT_TIMESTAMP
		FROM saby_nomenclature source
		WHERE source.saby_id = p.saby_id AND 'description' = ANY(p.saby_fields)
			AND source.missing_since IS NULL AND p.description <> source.description
	`); err != nil {
		return fmt.Errorf("update Saby descriptions: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE product_variants pv
		SET base_price_minor = source.price_minor, updated_at = CURRENT_TIMESTAMP
		FROM saby_nomenclature source, products p
		WHERE source.saby_id = pv.saby_id AND p.id = pv.product_id
			AND 'price' = ANY(p.saby_fields) AND source.missing_since IS NULL
			AND source.price_minor > 0 AND pv.base_price_minor <> source.price_minor
	`); err != nil {
		return fmt.Errorf("update Saby prices: %w", err)
	}

	if err := syncPhotos(ctx, tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit Saby sync: %w", err)
	}
	return nil
}

// syncPhotos переносит снимки только тем товарам, которым это разрешено, и
// только если набор действительно изменился: лишняя перезапись сбрасывала бы
// порядок фотографий на карточке на ровном месте.
func syncPhotos(ctx context.Context, tx pgx.Tx) error {
	type target struct {
		id      int64
		name    string
		wanted  []string
		current []string
	}
	rows, err := tx.Query(ctx, `
		SELECT p.id, p.name, source.images,
			ARRAY(
				SELECT pm.object_key FROM product_media pm
				WHERE pm.product_id = p.id
				ORDER BY pm.is_primary DESC, pm.sort_order, pm.id
			)
		FROM products p
		JOIN saby_nomenclature source ON source.saby_id = p.saby_id
		WHERE 'photo' = ANY(p.saby_fields) AND source.missing_since IS NULL
	`)
	if err != nil {
		return fmt.Errorf("query Saby photo targets: %w", err)
	}
	targets := make([]target, 0)
	for rows.Next() {
		var item target
		if err := rows.Scan(&item.id, &item.name, &item.wanted, &item.current); err != nil {
			rows.Close()
			return fmt.Errorf("scan Saby photo target: %w", err)
		}
		targets = append(targets, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("read Saby photo targets: %w", err)
	}
	rows.Close()

	for _, item := range targets {
		if len(item.wanted) == 0 || sameStrings(item.wanted, item.current) {
			continue
		}
		if _, err := tx.Exec(ctx, "DELETE FROM product_media WHERE product_id = $1", item.id); err != nil {
			return fmt.Errorf("replace Saby media %d: %w", item.id, err)
		}
		for index, image := range item.wanted {
			if _, err := tx.Exec(ctx, `
				INSERT INTO product_media (product_id, object_key, alt_text, sort_order, is_primary)
				VALUES ($1, $2, $3, $4, $5)
			`, item.id, image, item.name, index, boolToInt(index == 0)); err != nil {
				return fmt.Errorf("insert Saby media %d: %w", item.id, err)
			}
		}
	}
	return nil
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func normalizeItems(items []CatalogItem) []normalizedItem {
	result := make([]normalizedItem, 0, len(items))
	for _, item := range items {
		id := valueString(item.ID)
		// Цена может отсутствовать: в справочник попадает вся номенклатура
		// точки, а не только продаваемое из прайс-листа. Такой товар можно
		// завести в магазин и назначить цену самому.
		cost, _ := valueFloat(item.Cost)
		name := strings.TrimSpace(item.Name)
		if item.IsParent || (item.Published != nil && !*item.Published) ||
			id == "" || name == "" {
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
			id:          id,
			code:        itemCode(item),
			article:     valueString(item.Article),
			barcode:     valueString(item.Barcode),
			name:        name,
			description: strings.TrimSpace(item.Description),
			costMinor:   max(0, int64(math.Round(cost*100))),
			balance:     max(0, int(math.Floor(balance))),
			images:      images,
		})
	}
	return result
}

// itemCode — тот самый «код товара», по которому менеджер ищет позицию в
// СБИС (вида X1150532). Лежит он в nomNumber; соседние поля заняты
// внутренними идентификаторами, поэтому берём их лишь как запасной вариант.
//
// Опознаваемые GUID отбрасываем совсем: такой код человеку ни о чём не
// говорит, а в поиске по коду только мешает — он побеждал бы настоящий
// номер просто потому, что заполнен.
func itemCode(item CatalogItem) string {
	for _, candidate := range []any{item.NomNumber, item.Code, item.ExternalID, item.Article} {
		value := valueString(candidate)
		if value != "" && !looksLikeGUID(value) {
			return value
		}
	}
	return ""
}

func looksLikeGUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, symbol := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if symbol != '-' {
				return false
			}
			continue
		}
		hex := (symbol >= '0' && symbol <= '9') ||
			(symbol >= 'a' && symbol <= 'f') ||
			(symbol >= 'A' && symbol <= 'F')
		if !hex {
			return false
		}
	}
	return true
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
