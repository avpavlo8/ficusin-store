package saby

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/avpavlo8/ficusin-store/backend/internal/procurement"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	pool     *pgxpool.Pool
	verifier *OIDCVerifier
}

func (service *Service) SyncSales(ctx context.Context, upload SalesUpload) (SalesResult, error) {
	from, err := time.Parse("2006-01-02", strings.TrimSpace(upload.From))
	if err != nil {
		return SalesResult{}, errors.New("invalid Saby sales period")
	}
	to, err := time.Parse("2006-01-02", strings.TrimSpace(upload.To))
	if err != nil || from.After(to) || to.Sub(from) > 366*24*time.Hour || len(upload.Items) > 100000 {
		return SalesResult{}, errors.New("invalid Saby sales period")
	}
	records := make([]procurement.SalesRecord, 0, len(upload.Items))
	cards := make([]procurement.ChannelProduct, 0, len(upload.Items))
	for _, item := range upload.Items {
		date, parseErr := time.Parse("2006-01-02", strings.TrimSpace(item.Date))
		if parseErr != nil || strings.TrimSpace(item.SabyID) == "" {
			return SalesResult{}, errors.New("invalid Saby sales item")
		}
		records = append(records, procurement.SalesRecord{
			Date: date, ExternalID: item.SabyID, SabyID: item.SabyID,
			Units: item.Units, GrossRUB: item.GrossRUB,
		})
		if strings.TrimSpace(item.Name) != "" || strings.TrimSpace(item.Article) != "" {
			cards = append(cards, procurement.ChannelProduct{
				ExternalID: item.SabyID, Article: item.Article, Name: item.Name,
			})
		}
	}
	store := procurement.NewPostgresStore(service.pool)
	if len(cards) > 0 {
		if err := store.RememberChannelProducts(ctx, "saby", cards); err != nil {
			return SalesResult{}, fmt.Errorf("remember Saby sale names: %w", err)
		}
	}
	rows, err := store.ReplaceSales(ctx, "saby", from, to, records)
	if err != nil {
		return SalesResult{}, fmt.Errorf("replace Saby sales: %w", err)
	}
	var linkedRows, recommendationRows int
	if err := service.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE sale.saby_id IS NOT NULL)::INTEGER,
			COUNT(DISTINCT supplier_product.saby_id) FILTER (
				WHERE sale.saby_id IS NOT NULL AND nomenclature.balance < sale.units
			)::INTEGER
		FROM procurement_sales_daily sale
		LEFT JOIN procurement_supplier_products supplier_product ON supplier_product.saby_id = sale.saby_id
		LEFT JOIN saby_nomenclature nomenclature ON nomenclature.saby_id = sale.saby_id
		WHERE sale.channel = 'saby' AND sale.sale_date BETWEEN $1 AND $2
	`, from, to).Scan(&linkedRows, &recommendationRows); err != nil {
		return SalesResult{}, fmt.Errorf("verify linked Saby sales: %w", err)
	}
	return SalesResult{
		OK: true, Rows: rows, LinkedRows: linkedRows,
		RecommendationRows: recommendationRows, SyncedAt: time.Now().UTC(),
	}, nil
}

// normalizedItem — позиция номенклатуры, приведённая к нашему виду.
type normalizedItem struct {
	id          string
	code        string
	externalIDs []string
	article     string
	barcode     string
	barcodes    []string
	name        string
	description string
	costMinor   int64
	balance     int
	images      []string
	attributes  map[string]any
	sectionPath []string
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
	ExternalIDs []string `json:"external_ids"`
	Article     string   `json:"article"`
	Barcode     string   `json:"barcode"`
	Barcodes    []string `json:"barcodes"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	PriceMinor  int64    `json:"price_minor"`
	Balance     int      `json:"balance"`
	Images      []string         `json:"images"`
	Attributes  map[string]any   `json:"attributes"`
	SectionPath []string         `json:"section_path"`
}

// Kept as one statement so the live PostgreSQL test can execute the exact
// production mapping query after every migration has been applied.
const syncCharacteristicsSQL = `
	INSERT INTO product_attribute_values(product_id,attribute_id,value,source,updated_at)
	SELECT p.id,d.id,entry.value,'saby',CURRENT_TIMESTAMP
	FROM products p JOIN saby_nomenclature n ON n.saby_id=p.saby_id
	CROSS JOIN LATERAL jsonb_each(n.characteristics) entry
	JOIN attribute_definitions d ON d.code=entry.key
	WHERE entry.value NOT IN ('null'::jsonb,'""'::jsonb,'[]'::jsonb)
	  AND EXISTS (WITH RECURSIVE ancestors AS (
		SELECT p.category_id id UNION ALL SELECT c.parent_id FROM categories c JOIN ancestors a ON c.id=a.id WHERE c.parent_id IS NOT NULL
	  ) SELECT 1 FROM category_attributes ca JOIN ancestors a ON a.id=ca.category_id WHERE ca.attribute_id=d.id)
	  AND CASE d.data_type
		WHEN 'number' THEN jsonb_typeof(entry.value)='number'
		WHEN 'boolean' THEN jsonb_typeof(entry.value)='boolean'
		WHEN 'enum' THEN jsonb_typeof(entry.value)='string' AND EXISTS (
			SELECT 1 FROM attribute_options option
			WHERE option.attribute_id=d.id AND option.is_active
			  AND option.code=(entry.value #>> '{}')
		)
		WHEN 'multi_enum' THEN jsonb_typeof(entry.value)='array' AND NOT EXISTS (
			SELECT 1 FROM jsonb_array_elements_text(entry.value) value
			WHERE NOT EXISTS (
				SELECT 1 FROM attribute_options option
				WHERE option.attribute_id=d.id AND option.is_active AND option.code=value
			)
		)
		ELSE jsonb_typeof(entry.value)='string' END
	ON CONFLICT(product_id,attribute_id) DO UPDATE SET value=EXCLUDED.value,source='saby',updated_at=CURRENT_TIMESTAMP
	WHERE product_attribute_values.source='saby'
`

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

	var knownItems, knownPositive int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*)::INTEGER,
			COUNT(*) FILTER (WHERE balance > 0)::INTEGER
		FROM saby_nomenclature
		WHERE missing_since IS NULL
	`).Scan(&knownItems, &knownPositive); err != nil {
		return fmt.Errorf("read previous Saby catalogue health: %w", err)
	}
	if err := validateCatalogHealth(items, knownItems, knownPositive); err != nil {
		return err
	}

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
		// Keep the catalogue endpoint ID as the stock key. Existing storefront
		// variants were imported with this ID; replacing it with the human X-code
		// leaves every inventory row unmatched. The code remains available in its
		// own indexed column for procurement and manager-facing lookup.
		rows = append(rows, poolRow{
			SabyID: item.id, Code: item.code, ExternalIDs: item.externalIDs,
			Article: item.article, Barcode: item.barcode,
			Barcodes: item.barcodes,
			Name: item.name, Description: item.description,
			PriceMinor: item.costMinor, Balance: item.balance, Images: item.images, Attributes: item.attributes,
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
			saby_id, code, external_ids, article, barcode, barcodes, name, description,
			price_minor, balance, images, characteristics, section_path, seen_at, missing_since
		)
		SELECT item.saby_id, item.code,
			ARRAY(SELECT jsonb_array_elements_text(item.external_ids)), item.article, item.barcode,
			ARRAY(SELECT jsonb_array_elements_text(item.barcodes)), item.name,
			item.description, item.price_minor, item.balance,
			ARRAY(SELECT jsonb_array_elements_text(item.images)), item.attributes,
			ARRAY(SELECT jsonb_array_elements_text(item.section_path)),
			CURRENT_TIMESTAMP, NULL
		FROM jsonb_to_recordset($1::jsonb) AS item(
			saby_id TEXT, code TEXT, external_ids JSONB, article TEXT, barcode TEXT, barcodes JSONB,
			name TEXT, description TEXT, price_minor BIGINT, balance INTEGER, images JSONB, attributes JSONB,
			section_path JSONB
		)
		ON CONFLICT (saby_id) DO UPDATE SET
			code = EXCLUDED.code, external_ids = EXCLUDED.external_ids, article = EXCLUDED.article,
			barcode = EXCLUDED.barcode, barcodes = EXCLUDED.barcodes,
			name = EXCLUDED.name,
			description = EXCLUDED.description, price_minor = EXCLUDED.price_minor,
			balance = EXCLUDED.balance, images = EXCLUDED.images,
			section_path = EXCLUDED.section_path,
			characteristics = CASE WHEN EXCLUDED.characteristics='{}'::jsonb THEN saby_nomenclature.characteristics ELSE EXCLUDED.characteristics END,
			seen_at = CURRENT_TIMESTAMP, missing_since = NULL
	`, catalogue); err != nil {
		return fmt.Errorf("upsert Saby nomenclature: %w", err)
	}
	if _, err := tx.Exec(ctx, syncCharacteristicsSQL); err != nil {
		return fmt.Errorf("map Saby characteristics: %w", err)
	}

	// Refresh integration mappings independently from product identity. Empty
	// supplier values never delete a mapping that was already confirmed.
	if _, err := tx.Exec(ctx, `
		UPDATE product_external_ids external SET status='legacy',is_primary=FALSE,
			last_seen_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP
		FROM products product JOIN product_variants variant ON variant.product_id=product.id
		JOIN saby_nomenclature source ON source.saby_id=product.saby_id
		WHERE external.variant_id=variant.id AND external.provider='saby'
			AND external.id_type='id' AND external.external_id<>source.saby_id
	`); err != nil { return fmt.Errorf("retire changed Saby IDs: %w", err) }
	if _, err := tx.Exec(ctx, `
		INSERT INTO product_external_ids(product_id, variant_id, provider, id_type, external_id,status,is_primary,source,last_seen_at)
		SELECT p.id, pv.id, 'saby', 'id', source.saby_id,'active',TRUE,'sync',CURRENT_TIMESTAMP
		FROM products p
		JOIN saby_nomenclature source ON source.saby_id=p.saby_id
		JOIN product_variants pv ON pv.product_id=p.id AND pv.saby_id=source.saby_id
		ON CONFLICT(provider,id_type,external_id) DO UPDATE SET
			product_id=EXCLUDED.product_id, variant_id=EXCLUDED.variant_id,
			status='active',is_primary=TRUE,last_seen_at=CURRENT_TIMESTAMP,
			updated_at=CURRENT_TIMESTAMP
	`); err != nil {
		return fmt.Errorf("map Saby IDs: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE product_external_ids external SET status='legacy',is_primary=FALSE,
			last_seen_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP
		FROM products product JOIN product_variants variant ON variant.product_id=product.id
		JOIN saby_nomenclature source ON source.saby_id=product.saby_id
		WHERE external.variant_id=variant.id AND external.provider='saby'
			AND external.id_type='code' AND NULLIF(BTRIM(source.code),'') IS NOT NULL
			AND external.external_id<>source.code
	`); err != nil { return fmt.Errorf("retire changed Saby codes: %w", err) }
	if _, err := tx.Exec(ctx, `
		INSERT INTO product_external_ids(product_id, variant_id, provider, id_type, external_id,status,is_primary,source,last_seen_at)
		SELECT p.id, pv.id, 'saby', 'code', source.code,'active',TRUE,'sync',CURRENT_TIMESTAMP
		FROM products p
		JOIN saby_nomenclature source ON source.saby_id=p.saby_id
		JOIN product_variants pv ON pv.product_id=p.id AND pv.saby_id=source.saby_id
		WHERE NULLIF(BTRIM(source.code),'') IS NOT NULL
		ON CONFLICT(provider,id_type,external_id) DO UPDATE SET
			product_id=EXCLUDED.product_id, variant_id=EXCLUDED.variant_id,
			status='active',is_primary=TRUE,last_seen_at=CURRENT_TIMESTAMP,
			updated_at=CURRENT_TIMESTAMP
	`); err != nil {
		return fmt.Errorf("map Saby codes: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE product_external_ids external SET status='legacy',is_primary=FALSE,
			last_seen_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP
		FROM products product JOIN product_variants variant ON variant.product_id=product.id
		JOIN saby_nomenclature source ON source.saby_id=product.saby_id
		WHERE external.variant_id=variant.id AND external.provider='saby'
			AND external.id_type='alias'
			AND NOT (external.external_id=ANY(source.external_ids))
	`); err != nil { return fmt.Errorf("retire changed Saby aliases: %w", err) }
	if _, err := tx.Exec(ctx, `
		INSERT INTO product_external_ids(product_id, variant_id, provider, id_type, external_id,status,is_primary,source,last_seen_at)
		SELECT p.id, pv.id, 'saby', 'alias', alias.external_id,'active',FALSE,'sync',CURRENT_TIMESTAMP
		FROM products p
		JOIN saby_nomenclature source ON source.saby_id=p.saby_id
		JOIN product_variants pv ON pv.product_id=p.id AND pv.saby_id=source.saby_id
		CROSS JOIN LATERAL UNNEST(source.external_ids) alias(external_id)
		WHERE NULLIF(BTRIM(alias.external_id),'') IS NOT NULL
		ON CONFLICT(provider,id_type,external_id) DO UPDATE SET
			product_id=EXCLUDED.product_id, variant_id=EXCLUDED.variant_id,
			status='active',is_primary=FALSE,last_seen_at=CURRENT_TIMESTAMP,
			updated_at=CURRENT_TIMESTAMP
	`); err != nil {
		return fmt.Errorf("map Saby aliases: %w", err)
	}

	// Old sales may contain a Retail UUID or an X-code instead of catalogue.id.
	// Resolve them after every catalogue snapshot, including rows that were
	// loaded before the canonical directory existed.
	if _, err := tx.Exec(ctx, `
		WITH resolved AS (
			SELECT sale.channel, sale.sale_date, sale.external_product_id,
				directory.variant_id, directory.saby_id, mapping.id AS mapping_id
			FROM procurement_sales_daily sale
			JOIN LATERAL (
				SELECT external.id, external.variant_id
				FROM product_external_ids external
				WHERE external.provider='saby'
					AND external.id_type IN ('id','code','alias')
					AND external.external_id=sale.external_product_id
					AND external.status IN ('active','legacy')
				ORDER BY (external.status='active') DESC, external.updated_at DESC
				LIMIT 1
			) mapping ON TRUE
			JOIN canonical_product_directory directory ON directory.variant_id=mapping.variant_id
			JOIN products product ON product.id=directory.product_id AND product.catalog_section='plants'
			WHERE sale.channel='saby'
		)
		UPDATE procurement_sales_daily sale SET
			saby_id=resolved.saby_id, canonical_variant_id=resolved.variant_id,
			external_mapping_id=resolved.mapping_id, synced_at=CURRENT_TIMESTAMP
		FROM resolved
		WHERE sale.channel=resolved.channel AND sale.sale_date=resolved.sale_date
			AND sale.external_product_id=resolved.external_product_id
	`); err != nil {
		return fmt.Errorf("repair Saby sales aliases: %w", err)
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

// validateCatalogHealth prevents a transient or truncated Saby response from
// turning a healthy local catalogue into an empty shop. A real large stock
// reduction can still be imported once the source returns the complete list;
// only structurally implausible snapshots are rejected.
func validateCatalogHealth(items []normalizedItem, knownItems, knownPositive int) error {
	if knownItems >= 20 && len(items)*2 < knownItems {
		return fmt.Errorf("unsafe Saby catalog: received %d of %d known items", len(items), knownItems)
	}
	positive := 0
	for _, item := range items {
		if item.balance > 0 {
			positive++
		}
	}
	if knownPositive >= 5 && positive == 0 {
		return fmt.Errorf("unsafe Saby catalog: all %d item balances are zero", len(items))
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
		// `published` belongs to the Saby sales channel. It must not decide
		// whether an existing Ficusin card receives its stock: publication on
		// the storefront is managed by Ficusin itself.
		if item.IsParent || id == "" || name == "" {
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
			externalIDs: itemExternalIDs(item),
			article:     valueString(item.Article),
			barcode:     valueString(item.Barcode),
			barcodes:    catalogBarcodes(item),
			name:        name,
			description: plainDescription(item.Description),
			costMinor:   max(0, int64(math.Round(cost*100))),
			balance:     max(0, int(math.Floor(balance))),
			images:      images,
			attributes:  normalizeCharacteristics(item.Attributes),
			sectionPath: normalizeSectionPath(item.SectionPath),
		})
	}
	return result
}

func normalizeSectionPath(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		key := strings.ToLower(value)
		if value == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, value)
	}
	return result
}

func itemExternalIDs(item CatalogItem) []string {
	seen := map[string]bool{}
	result := make([]string, 0, 3)
	primaryID, code := valueString(item.ID), itemCode(item)
	for _, candidate := range []any{item.ExternalID, item.HierarchicalID, item.UUID} {
		value := valueString(candidate)
		if value == "" || value == primaryID || value == code || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

var characteristicCodes = map[string]string{
	"height": "height_cm", "heightcm": "height_cm", "высота": "height_cm",
	"potdiameter": "pot_diameter_cm", "potdiametercm": "pot_diameter_cm", "диаметргоршка": "pot_diameter_cm",
	"lightlevel": "light_level", "освещение": "light_level", "watering": "watering", "полив": "watering",
	"humidity": "humidity", "влажность": "humidity", "carelevel": "care_level", "сложностьухода": "care_level",
	"toxicity": "toxicity", "токсичность": "toxicity", "petsafety": "pet_safety", "безопасностьдляживотных": "pet_safety",
	"placement": "placement", "помещения": "placement", "growthhabit": "growth_habit", "формароста": "growth_habit",
}

func normalizeCharacteristics(raw any) map[string]any {
	result := map[string]any{}
	allowed := map[string]bool{"height_cm": true, "pot_diameter_cm": true, "light_level": true, "watering": true, "humidity": true, "care_level": true, "toxicity": true, "pet_safety": true, "placement": true, "growth_habit": true}
	add := func(name string, value any) {
		key := normalizeCharacteristicName(name)
		code := characteristicCodes[key]
		if code == "" { code = key }
		if !allowed[code] { return }
		if code == "height_cm" || code == "pot_diameter_cm" {
			if number, ok := valueFloat(value); ok && number > 0 { result[code] = number }
			return
		}
		switch typed := value.(type) {
		case string:
			if text := strings.ToLower(strings.TrimSpace(typed)); text != "" { result[code] = text }
		case []any:
			values := []string{}
			for _, item := range typed { if text := strings.ToLower(valueString(item)); text != "" { values = append(values, text) } }
			if len(values) > 0 { result[code] = values }
		case []string:
			if len(typed) > 0 { result[code] = typed }
		}
	}
	switch typed := raw.(type) {
	case map[string]any:
		for name, value := range typed { add(name, characteristicValue(value)) }
	case []any:
		for _, entry := range typed {
			if item, ok := entry.(map[string]any); ok { add(valueString(firstValue(item, "code", "name", "title", "characteristic")), characteristicValue(firstValue(item, "value", "values", "text"))) }
		}
	}
	return result
}

func normalizeCharacteristicName(value string) string {
	return strings.Map(func(r rune) rune { if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' { return unicode.ToLower(r) }; return -1 }, value)
}
func firstValue(values map[string]any, keys ...string) any { for _, key := range keys { if value, ok := values[key]; ok { return value } }; return nil }
func characteristicValue(value any) any { if object, ok := value.(map[string]any); ok { return firstValue(object, "value", "values", "text") }; return value }

var (
	descriptionBreaks = regexp.MustCompile(`(?i)<\s*(br\s*/?|/p|/div|/li)\s*>`)
	descriptionTags   = regexp.MustCompile(`<[^>]*>`)
	descriptionSpace  = regexp.MustCompile(`[\t\r ]+`)
	descriptionLines  = regexp.MustCompile(`\n{3,}`)
)

// plainDescription turns Saby's editor HTML into readable text. The
// storefront deliberately renders text rather than trusting third-party
// markup, so sanitising here both preserves paragraphs and prevents raw P/BR
// tags from leaking into product cards.
func plainDescription(value string) string {
	value = html.UnescapeString(strings.TrimSpace(value))
	value = strings.Map(func(symbol rune) rune {
		if symbol != '\n' && unicode.IsSpace(symbol) {
			return ' '
		}
		return symbol
	}, value)
	value = descriptionBreaks.ReplaceAllString(value, "\n")
	value = descriptionTags.ReplaceAllString(value, " ")
	value = descriptionSpace.ReplaceAllString(value, " ")
	lines := strings.Split(value, "\n")
	clean := lines[:0]
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.EqualFold(line, "p") {
			clean = append(clean, line)
		}
	}
	return strings.TrimSpace(descriptionLines.ReplaceAllString(strings.Join(clean, "\n"), "\n\n"))
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

// catalogBarcodes собирает все штрихкоды позиции. У растения их обычно три:
// два EAN13 с этикетки и код, выданный маркетплейсом. Именно последний и
// связывает товар с карточкой площадки, поэтому берём все и не выбираем.
func catalogBarcodes(item CatalogItem) []string {
	codes := make([]string, 0, len(item.Barcodes))
	seen := make(map[string]bool, len(item.Barcodes))
	for _, barcode := range item.Barcodes {
		code := strings.TrimSpace(valueString(barcode.Code))
		if code == "" || seen[code] {
			continue
		}
		seen[code] = true
		codes = append(codes, code)
	}
	return codes
}
