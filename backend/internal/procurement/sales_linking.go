package procurement

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// manualSalesChannels — каналы, у которых внешний код продажи может
// разойтись с номенклатурой. Сайт и СБИС кладут в продажи сам saby_id,
// связывать там нечего.
var manualSalesChannels = []string{"wb", "ozon", "saby"}

// unlinkedSalesLimit — сколько внешних кодов отдаём на экран за раз.
//
// Разбор ручной, дальше третьей сотни за один заход никто не уходит, а
// без потолка редкий канал с тысячами разовых кодов повесил бы таблицу.
const unlinkedSalesLimit = 300

// linkableCandidatesLimit — сколько кандидатов показываем при поиске.
const linkableCandidatesLimit = 40

// UnlinkedSale — внешний код, продажи которого не дошли до товара.
//
// Строка агрегирована по коду, а не по дню: разбирающему важно, сколько
// всего продано под этим кодом и когда его видели в последний раз, —
// давно замолчавший код чаще всего снятая карточка, и его можно отложить.
//
// Article и Name — подпись карточки с площадки. Без неё разбор Wildberries
// слепой: там внешний код — это числовой nmID.
type UnlinkedSale struct {
	Channel    string  `json:"channel"`
	ExternalID string  `json:"externalId"`
	Article    string  `json:"article"`
	Name       string  `json:"name"`
	Days       int     `json:"days"`
	Units      int     `json:"units"`
	GrossRUB   float64 `json:"grossRub"`
	LastSale   string  `json:"lastSale"`
	Ignored    bool    `json:"ignored"`
}

// SalesLink — решение человека: этот внешний код принадлежит этому товару.
type SalesLink struct {
	Channel    string `json:"channel"`
	ExternalID string `json:"externalId"`
	VariantID  int64  `json:"variantId"`
	// SabyID remains accepted for one compatible release. New clients send
	// VariantID and never bind marketplace history directly to a remote ID.
	SabyID string `json:"sabyId"`
}

// SalesLinkResult — что изменилось после связывания.
//
// Одного «сохранено» мало: человек разбирает список ради расчёта закупки и
// должен видеть, сколько продаж вернулось в расчёт и сколько кодов ещё
// ждут. TakenFrom не пустой, когда код пришлось отобрать у другого товара.
type SalesLinkResult struct {
	Channel     string `json:"channel"`
	ExternalID  string `json:"externalId"`
	SabyID      string `json:"sabyId"`
	SabyName    string `json:"sabyName"`
	LinkedRows  int    `json:"linkedRows"`
	LinkedUnits int    `json:"linkedUnits"`
	TakenFrom   string `json:"takenFrom"`
	Remaining   int    `json:"remaining"`
}

// SalesLinkStore — необязательное дополнение к Store: разбор продаж, которым
// не нашлось товара.
//
// Отдельным интерфейсом с приведением типа, как SalesDiagnostics у источника
// продаж. Store реализуют ещё и заглушки тестов, поэтому каждый метод,
// добавленный в него, стоит правки во всех сразу и связан с задачей только
// тем, что тоже ходит в базу.
type SalesLinkStore interface {
	ListUnlinkedSales(context.Context, string, int) ([]UnlinkedSale, error)
	LinkSalesProduct(context.Context, Actor, SalesLink) (SalesLinkResult, error)
	SearchLinkableNomenclature(context.Context, string) ([]NomenclatureCandidate, error)
	RememberChannelProducts(context.Context, string, []ChannelProduct) error
}

type SalesIgnoreStore interface {
	IgnoreSalesProduct(context.Context, Actor, string, string, bool) error
	ListUnlinkedSalesIncludingIgnored(context.Context, string, int) ([]UnlinkedSale, error)
}

// ErrSalesLinkUnsupported — хранилище не умеет разбирать продажи руками.
var ErrSalesLinkUnsupported = errors.New("procurement store cannot link sales manually")

// UnlinkedSales отдаёт коды канала, оставшиеся без товара.
func (service *Service) UnlinkedSales(ctx context.Context, channel string) ([]UnlinkedSale, error) {
	return service.unlinkedSales(ctx, channel, false)
}

func (service *Service) UnlinkedSalesWithIgnored(ctx context.Context, channel string) ([]UnlinkedSale, error) {
	return service.unlinkedSales(ctx, channel, true)
}

func (service *Service) unlinkedSales(ctx context.Context, channel string, showIgnored bool) ([]UnlinkedSale, error) {
	channel = strings.TrimSpace(channel)
	if !oneOf(channel, manualSalesChannels...) {
		return nil, ErrInvalidInput
	}
	store, able := service.store.(SalesLinkStore)
	if !able {
		return nil, ErrSalesLinkUnsupported
	}
	if showIgnored {
		if ignoredStore, ok := service.store.(SalesIgnoreStore); ok {
			return ignoredStore.ListUnlinkedSalesIncludingIgnored(ctx, channel, unlinkedSalesLimit)
		}
	}
	return store.ListUnlinkedSales(ctx, channel, unlinkedSalesLimit)
}

func (service *Service) IgnoreSalesProduct(ctx context.Context, actor Actor, channel, externalID string, ignored bool) error {
	channel, externalID = strings.TrimSpace(channel), strings.TrimSpace(externalID)
	if !oneOf(channel, manualSalesChannels...) || externalID == "" || len(externalID) > 200 {
		return ErrInvalidInput
	}
	store, able := service.store.(SalesIgnoreStore)
	if !able {
		return ErrSalesLinkUnsupported
	}
	return store.IgnoreSalesProduct(ctx, actor, channel, externalID, ignored)
}

// SearchLinkableNomenclature ищет товар, за которым можно закрепить код.
//
// От общего поиска по справочнику отличается тем, что не показывает
// позиции, пропавшие из выгрузки СБИС. Выбрать такую — значит приписать
// продажи карточке, которой в магазине уже нет, и потерять их в расчёте
// закупки во второй раз.
func (service *Service) SearchLinkableNomenclature(ctx context.Context, query string) ([]NomenclatureCandidate, error) {
	query = strings.TrimSpace(query)
	if len([]rune(query)) < 2 || len(query) > 200 {
		return nil, ErrInvalidInput
	}
	store, able := service.store.(SalesLinkStore)
	if !able {
		return nil, ErrSalesLinkUnsupported
	}
	return store.SearchLinkableNomenclature(ctx, query)
}

// LinkSalesProduct закрепляет внешний код за нашим каноническим SKU.
func (service *Service) LinkSalesProduct(ctx context.Context, actor Actor, input SalesLink) (SalesLinkResult, error) {
	input.Channel = strings.TrimSpace(input.Channel)
	input.ExternalID = strings.TrimSpace(input.ExternalID)
	input.SabyID = strings.TrimSpace(input.SabyID)
	if !oneOf(input.Channel, manualSalesChannels...) || input.ExternalID == "" || (input.VariantID <= 0 && input.SabyID == "") ||
		len(input.ExternalID) > 200 || len(input.SabyID) > 200 {
		return SalesLinkResult{}, ErrInvalidInput
	}
	// У Wildberries внешний код продажи — это nmID, и хранится он числом.
	// Текст в этом поле означает, что связывать собрались не тот код, и
	// молча положить его в базу нельзя: приведение упадёт уже в запросе.
	if input.Channel == "wb" {
		if _, err := strconv.ParseInt(input.ExternalID, 10, 64); err != nil {
			return SalesLinkResult{}, ErrInvalidInput
		}
	}
	store, able := service.store.(SalesLinkStore)
	if !able {
		return SalesLinkResult{}, ErrSalesLinkUnsupported
	}
	return store.LinkSalesProduct(ctx, actor, input)
}

func (store *PostgresStore) ListUnlinkedSales(ctx context.Context, channel string, limit int) ([]UnlinkedSale, error) {
	return store.listUnlinkedSales(ctx, channel, limit, false)
}

func (store *PostgresStore) ListUnlinkedSalesIncludingIgnored(ctx context.Context, channel string, limit int) ([]UnlinkedSale, error) {
	return store.listUnlinkedSales(ctx, channel, limit, true)
}

func (store *PostgresStore) listUnlinkedSales(ctx context.Context, channel string, limit int, includeIgnored bool) ([]UnlinkedSale, error) {
	// Подпись карточки берётся слева: площадку могли ещё ни разу не
	// прочитать, и это не повод прятать продажи — код покажем как есть.
	rows, err := store.pool.Query(ctx, `
		SELECT sale.external_product_id, COUNT(*)::INTEGER,
			COALESCE(SUM(sale.units), 0)::INTEGER,
			COALESCE(SUM(sale.gross_rub), 0)::DOUBLE PRECISION,
			MAX(sale.sale_date)::TEXT,
			COALESCE(MAX(NULLIF(card.article,'')), MAX(saby_card.article), ''),
			COALESCE(MAX(NULLIF(card.name,'')), MAX(saby_card.name), ''),
			BOOL_OR(ignored.external_product_id IS NOT NULL)
		FROM procurement_sales_daily sale
		LEFT JOIN procurement_channel_products card
			ON card.channel = sale.channel AND card.external_id = sale.external_product_id
		LEFT JOIN LATERAL (
			SELECT nomenclature.article, nomenclature.name
			FROM saby_nomenclature nomenclature
			WHERE sale.channel='saby' AND (
				nomenclature.saby_id=sale.external_product_id
				OR nomenclature.code=sale.external_product_id
				OR sale.external_product_id=ANY(nomenclature.external_ids)
			)
			ORDER BY (nomenclature.missing_since IS NULL) DESC, nomenclature.seen_at DESC
			LIMIT 1
		) saby_card ON TRUE
		LEFT JOIN procurement_ignored_sales_products ignored
			ON ignored.channel = sale.channel AND ignored.external_product_id = sale.external_product_id
		WHERE sale.channel = $1 AND sale.canonical_variant_id IS NULL
			AND ($3 OR ignored.external_product_id IS NULL)
		GROUP BY sale.external_product_id
		ORDER BY SUM(sale.units) DESC, MAX(sale.sale_date) DESC
		LIMIT $2
	`, channel, limit, includeIgnored)
	if err != nil {
		return nil, fmt.Errorf("list unlinked sales: %w", err)
	}
	defer rows.Close()
	items := make([]UnlinkedSale, 0, 64)
	for rows.Next() {
		item := UnlinkedSale{Channel: channel}
		if err := rows.Scan(
			&item.ExternalID, &item.Days, &item.Units, &item.GrossRUB, &item.LastSale,
			&item.Article, &item.Name, &item.Ignored,
		); err != nil {
			return nil, fmt.Errorf("scan unlinked sales: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store *PostgresStore) IgnoreSalesProduct(ctx context.Context, actor Actor, channel, externalID string, ignored bool) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil { return fmt.Errorf("begin ignore sales product: %w", err) }
	defer tx.Rollback(ctx) //nolint:errcheck
	if ignored {
		if _, err = tx.Exec(ctx, `INSERT INTO procurement_ignored_sales_products(channel, external_product_id, ignored_by)
			VALUES ($1,$2,$3) ON CONFLICT(channel,external_product_id) DO UPDATE SET ignored_by=EXCLUDED.ignored_by, ignored_at=CURRENT_TIMESTAMP`, channel, externalID, actor.CustomerID); err != nil {
			return fmt.Errorf("ignore sales product: %w", err)
		}
	} else if _, err = tx.Exec(ctx, `DELETE FROM procurement_ignored_sales_products WHERE channel=$1 AND external_product_id=$2`, channel, externalID); err != nil {
		return fmt.Errorf("restore sales product: %w", err)
	}
	if err := audit(ctx, tx, actor, "procurement.sales.ignore", "procurement_sales_daily", 0, map[string]any{"channel": channel, "externalId": externalID, "ignored": ignored}); err != nil { return err }
	return tx.Commit(ctx)
}

// SearchLinkableNomenclature отдаёт позиции нашего канонического справочника.
//
// Сортировка по остатку не косметика: у растения нередко заведено две
// карточки — рабочая и оставшаяся с прошлой выгрузки, — и различить их
// по названию нельзя. Та, на которой лежит товар, и есть действующая.
func (store *PostgresStore) SearchLinkableNomenclature(ctx context.Context, query string) ([]NomenclatureCandidate, error) {
	patterns := make([]string, 0, 8)
	stopWords := map[string]bool{
		"цветок": true, "цветы": true, "горшке": true, "горшок": true,
		"живой": true, "живое": true, "растение": true, "растения": true,
		"микс": true, "mix": true, "асс": true, "штука": true,
	}
	for _, token := range strings.FieldsFunc(strings.ToLower(query), func(r rune) bool { return !(r >= 'а' && r <= 'я') && !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') }) {
		if len([]rune(token)) >= 3 && !stopWords[token] {
			patterns = append(patterns, "%"+token+"%")
		}
	}
	if len(patterns) == 0 { patterns = append(patterns, "%"+query+"%") }
	rows, err := store.pool.Query(ctx, `
		SELECT directory.variant_id, directory.saby_id, directory.master_code,
			COALESCE(nomenclature.article, ''), directory.name,
			COALESCE(nomenclature.balance, 0),
			COALESCE(nomenclature.price_minor, 0)::DOUBLE PRECISION / 100
		FROM canonical_product_directory directory
		LEFT JOIN saby_nomenclature nomenclature ON nomenclature.saby_id = directory.saby_id
		WHERE directory.active AND directory.master_code <> ''
			AND (directory.name ILIKE '%' || $1 || '%'
				OR directory.master_code ILIKE '%' || $1 || '%'
				OR COALESCE(nomenclature.article, '') ILIKE '%' || $1 || '%'
				OR directory.saby_id ILIKE '%' || $1 || '%'
				OR directory.name ILIKE ANY($3::TEXT[]))
		ORDER BY (directory.master_code = UPPER($1)) DESC,
			(directory.name ILIKE '%' || $1 || '%') DESC,
			(SELECT COUNT(*) FROM UNNEST($3::TEXT[]) pattern WHERE directory.name ILIKE pattern) DESC,
			COALESCE(nomenclature.balance, 0) DESC, directory.name, directory.variant_id
		LIMIT $2
	`, query, linkableCandidatesLimit, patterns)
	if err != nil {
		return nil, fmt.Errorf("search linkable nomenclature: %w", err)
	}
	defer rows.Close()
	items := make([]NomenclatureCandidate, 0, linkableCandidatesLimit)
	for rows.Next() {
		var item NomenclatureCandidate
		if err := rows.Scan(&item.VariantID, &item.SabyID, &item.Code, &item.Article, &item.Name, &item.Balance, &item.Price); err != nil {
			return nil, fmt.Errorf("scan linkable nomenclature: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// RememberChannelProducts сохраняет подписи карточек площадки.
//
// Это вспомогательная запись: она ничего не связывает и не участвует в
// расчёте, поэтому и хранится отдельно от procurement_product_channels.
func (store *PostgresStore) RememberChannelProducts(ctx context.Context, channel string, items []ChannelProduct) error {
	if !oneOf(channel, manualSalesChannels...) {
		return ErrInvalidInput
	}
	batch := &pgx.Batch{}
	for _, item := range items {
		externalID := strings.TrimSpace(item.ExternalID)
		if externalID == "" {
			continue
		}
		barcodes := item.Barcodes
		if barcodes == nil {
			barcodes = []string{}
		}
		batch.Queue(`
			INSERT INTO procurement_channel_products (
				channel, external_id, article, name, barcodes, current_price, current_base_price
			) VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (channel, external_id) DO UPDATE SET
				article = EXCLUDED.article, name = EXCLUDED.name,
				barcodes = EXCLUDED.barcodes,
				current_price = COALESCE(EXCLUDED.current_price, procurement_channel_products.current_price),
				current_base_price = COALESCE(EXCLUDED.current_base_price, procurement_channel_products.current_base_price),
				seen_at = CURRENT_TIMESTAMP
		`, channel, externalID, strings.TrimSpace(item.Article), strings.TrimSpace(item.Name),
			barcodes, item.CurrentPrice, item.CurrentBasePrice)
	}
	if batch.Len() == 0 {
		return nil
	}
	results := store.pool.SendBatch(ctx, batch)
	defer results.Close() //nolint:errcheck
	for index := 0; index < batch.Len(); index++ {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("remember channel product: %w", err)
		}
	}
	return nil
}

// LinkSalesProduct связывает код канала с товаром и чинит уже загруженные
// продажи.
//
// Одной записи в справочник каналов мало: saby_id проставляется только при
// вставке продажи, поэтому без засыпки задним числом разобранная строка
// вернулась бы в расчёт лишь после следующей глубокой выгрузки, а более
// старая — никогда.
func (store *PostgresStore) LinkSalesProduct(ctx context.Context, actor Actor, input SalesLink) (SalesLinkResult, error) {
	result := SalesLinkResult{Channel: input.Channel, ExternalID: input.ExternalID, SabyID: input.SabyID}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return SalesLinkResult{}, fmt.Errorf("begin sales link: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var productID, variantID int64
	err = tx.QueryRow(ctx, `
		SELECT product_id, variant_id, saby_id, name
		FROM canonical_product_directory
		WHERE active AND (($1 > 0 AND variant_id = $1) OR ($1 <= 0 AND saby_id = $2))
		ORDER BY ($1 > 0 AND variant_id = $1) DESC, variant_id
		LIMIT 1
	`, input.VariantID, input.SabyID).Scan(&productID, &variantID, &result.SabyID, &result.SabyName)
	if errors.Is(err, pgx.ErrNoRows) {
		return SalesLinkResult{}, ErrNotFound
	}
	if err != nil {
		return SalesLinkResult{}, fmt.Errorf("load nomenclature for sales link: %w", err)
	}

	provider, idType := "ozon", "offer_id"
	if input.Channel == "wb" { provider, idType = "wildberries", "sku" }
	err = tx.QueryRow(ctx, `
		SELECT COALESCE(directory.master_code, directory.saby_id)
		FROM product_external_ids external
		JOIN canonical_product_directory directory ON directory.variant_id = external.variant_id
		WHERE external.provider = $1 AND external.id_type = $2
			AND external.external_id = $3 AND external.variant_id <> $4
	`, provider, idType, input.ExternalID, variantID).Scan(&result.TakenFrom)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return SalesLinkResult{}, fmt.Errorf("release channel code: %w", err)
	}
	var mappingID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO product_external_ids(product_id, variant_id, provider, id_type,
			external_id, status, is_primary, source, linked_by, confirmed_at, last_seen_at)
		VALUES($1,$2,$3,$4,$5,'active',FALSE,'manual',$6,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)
		ON CONFLICT(provider,id_type,external_id) DO UPDATE SET
			product_id=EXCLUDED.product_id, variant_id=EXCLUDED.variant_id,
			status='active', is_primary=FALSE, source='manual', linked_by=EXCLUDED.linked_by,
			confirmed_at=CURRENT_TIMESTAMP, last_seen_at=CURRENT_TIMESTAMP,
			updated_at=CURRENT_TIMESTAMP
		RETURNING id
	`, productID, variantID, provider, idType, input.ExternalID, actor.CustomerID).Scan(&mappingID)
	if err != nil {
		return SalesLinkResult{}, fmt.Errorf("save channel code: %w", err)
	}
	// WB's numeric nmID is only the API join key. Store the seller article
	// next to it, because this is the identifier people see and report on.
	if input.Channel == "wb" {
		_, err = tx.Exec(ctx, `
			INSERT INTO product_external_ids(product_id,variant_id,provider,id_type,
				external_id,status,is_primary,source,linked_by,confirmed_at,last_seen_at)
			SELECT $1,$2,'wildberries','vendor_code',BTRIM(article),'active',FALSE,
				'manual',$4,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP
			FROM procurement_channel_products
			WHERE channel='wb' AND external_id=$3 AND BTRIM(article)<>''
			ON CONFLICT(provider,id_type,external_id) DO UPDATE SET
				product_id=EXCLUDED.product_id, variant_id=EXCLUDED.variant_id,
				status='active', is_primary=FALSE, source='manual', linked_by=EXCLUDED.linked_by,
				confirmed_at=CURRENT_TIMESTAMP, last_seen_at=CURRENT_TIMESTAMP,
				updated_at=CURRENT_TIMESTAMP
		`, productID, variantID, input.ExternalID, actor.CustomerID)
		if err != nil { return SalesLinkResult{}, fmt.Errorf("save Wildberries article: %w", err) }
	}

	command, err := tx.Exec(ctx, `
		UPDATE procurement_sales_daily SET saby_id = $3,
			canonical_variant_id = $4, external_mapping_id = $5
		WHERE channel = $1 AND external_product_id = $2
			AND (canonical_variant_id IS DISTINCT FROM $4 OR saby_id IS DISTINCT FROM $3)
	`, input.Channel, input.ExternalID, result.SabyID, variantID, mappingID)
	if err != nil {
		return SalesLinkResult{}, fmt.Errorf("backfill linked sales: %w", err)
	}
	result.LinkedRows = int(command.RowsAffected())

	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(units), 0)::INTEGER FROM procurement_sales_daily
		WHERE channel = $1 AND external_product_id = $2
	`, input.Channel, input.ExternalID).Scan(&result.LinkedUnits); err != nil {
		return SalesLinkResult{}, fmt.Errorf("count linked sales: %w", err)
	}
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(DISTINCT external_product_id)::INTEGER FROM procurement_sales_daily
		WHERE channel = $1 AND canonical_variant_id IS NULL AND NOT EXISTS (
			SELECT 1 FROM procurement_ignored_sales_products ignored
			WHERE ignored.channel = procurement_sales_daily.channel
				AND ignored.external_product_id = procurement_sales_daily.external_product_id)
	`, input.Channel).Scan(&result.Remaining); err != nil {
		return SalesLinkResult{}, fmt.Errorf("count remaining sales: %w", err)
	}
	if err := audit(ctx, tx, actor, "procurement.sales.link", "procurement_sales_daily", 0, result); err != nil {
		return SalesLinkResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SalesLinkResult{}, fmt.Errorf("commit sales link: %w", err)
	}
	return result, nil
}
